package http

import (
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

func TestWebhookCaptureTypedBodyReaders(t *testing.T) {
	for _, mode := range []string{"raw", "json", "multipart"} {
		t.Run(mode, func(t *testing.T) {
			recorder := captureTestRecorder(t)
			var registration RouteRegistration
			var wire []byte
			var media string
			called := false
			path := "/wh/" + mode
			route := DefineRoute(RouteSpec{ID: "capture." + RouteID(mode), Method: POST, Access: Public}, StaticPath(path))
			switch mode {
			case "raw":
				wire, media = []byte("a\x00\xff\r\nb"), "application/octet-stream"
				endpoint := DefineEndpoint(route, EmptyQuery(), RawRequestBody("application/octet-stream"), EmptyResponse(204))
				registration = endpoint.Handle(func(_ context.Context, in Input[NoPath, NoQuery, RawBody]) (NoContent, error) {
					called = true
					got, err := io.ReadAll(in.Body)
					length, known := in.Body.Length().Get()
					if err != nil || !bytes.Equal(got, wire) || !known || length != int64(len(wire)) {
						t.Error("RawRequestBody changed", err)
					}
					return NoContent{}, nil
				})
			case "json":
				wire, media = []byte(" { \"name\":\"exact\", \"note\":null }\r\n"), "application/json"
				endpoint := DefineEndpoint(route, EmptyQuery(), JSONBody(endpointPatchJSON()), EmptyResponse(204))
				registration = endpoint.Handle(func(_ context.Context, in Input[NoPath, NoQuery, EndpointPatch]) (NoContent, error) {
					called = true
					if in.Body.Name != "exact" {
						t.Error("JSON decoder lost body")
					}
					return NoContent{}, nil
				})
			case "multipart":
				directory := t.TempDir()
				wire, media = multipartWire(t, multipartTestPart{name: "name", contents: "exact"}, primaryUpload("a\x00b\r\n"))
				endpoint := DefineEndpoint(route, EmptyQuery(), MultipartBody(multipartForm(directory)), EmptyResponse(204))
				registration = endpoint.Handle(func(_ context.Context, in multipartRequestInput) (NoContent, error) {
					called = true
					name, _ := in.Body.Name.Get()
					if name != "exact" || in.Body.Primary.Size() != 5 {
						t.Error("multipart changed")
					}
					return NoContent{}, nil
				})
				t.Cleanup(func() { assertMultipartCleanup(t, directory) })
			}
			router, err := NewRouter(registration)
			if err != nil {
				t.Fatal(err)
			}
			handler := captureTestHandler(t, router, DefaultServerConfig(), recorder)
			r := httptest.NewRequest("POST", path, bytes.NewReader(wire))
			r.Header.Set("Content-Type", media)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			s := captureTestTake(t, recorder)
			if !called || response.Code != stdhttp.StatusNoContent || !bytes.Equal(s.Body.Prefix, wire) || s.Body.End != httpcapture.EOF {
				t.Fatal("typed body capture mismatch", response.Code, response.Body.String())
			}
		})
	}
}

func TestWebhookCaptureDeclarationValidationAndGuard(t *testing.T) {
	r := captureTestRecorder(t)
	root, _ := ParsePathPrefix("/")
	wh, _ := ParsePathPrefix("/wh")
	for _, declarations := range [][]CaptureDeclaration{
		nil, {CaptureDeclaration{}}, {CapturePrefix(root)}, {CapturePrefix(wh), CapturePrefix(wh, FullCaptureBody())}, make([]CaptureDeclaration, 33),
	} {
		if _, err := configureServer([]ServerOption{WithWebhookCapture(r, declarations...)}); err == nil {
			t.Fatal("invalid capture declaration accepted")
		}
		if ValidateCaptureDeclarations(declarations...) == nil {
			t.Fatal("shared capture validation accepted an invalid declaration")
		}
	}
	if _, err := configureServer([]ServerOption{WithWebhookCapture(nil, CapturePrefix(wh))}); err == nil {
		t.Fatal("nil recorder accepted")
	}
	option := WithWebhookCapture(r, CapturePrefix(wh))
	if _, err := configureServer([]ServerOption{option, option}); err == nil {
		t.Fatal("multiple recorders accepted")
	}
	// In-package metadata is sufficient to test the shared preparation guard;
	// authentication's credential/actor contracts are exercised by its owner.
	router := &Router{routes: []RouteInfo{{Path: "/wh/private", Authentication: &AuthenticationInfo{}}}}
	wrapped, err := ApplyMiddleware(router, DefineMiddleware("capture.test", func(next stdhttp.Handler) (stdhttp.Handler, error) { return stdhttp.HandlerFunc(next.ServeHTTP), nil }))
	if err != nil {
		t.Fatal(err)
	}
	options, _ := configureServer([]ServerOption{option})
	for _, handler := range []stdhttp.Handler{router, wrapped} {
		if options.capture.validateRoutes(handler) == nil {
			t.Fatal("authenticated route accepted beneath capture prefix")
		}
	}
	for _, pattern := range []string{"/{tenant}/callback", "/{rest...}", "/wh/{rest...}"} {
		router := &Router{routes: []RouteInfo{{Path: pattern, Authentication: &AuthenticationInfo{}}}}
		if options.capture.validateRoutes(router) == nil {
			t.Fatal("overlapping authenticated pattern accepted", pattern)
		}
	}
	for _, pattern := range []string{"/whx/{rest...}", "/admin/{tenant}"} {
		router := &Router{routes: []RouteInfo{{Path: pattern, Authentication: &AuthenticationInfo{}}}}
		if err := options.capture.validateRoutes(router); err != nil {
			t.Fatal("unrelated guarded route rejected", pattern, err)
		}
	}
}
