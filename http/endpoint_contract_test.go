package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

type EndpointDecodedBody struct{}

func (*EndpointDecodedBody) UnmarshalJSON(data []byte) error {
	if strings.Contains(string(data), "goexit") {
		runtime.Goexit()
	}
	panic("private-decode-detail")
}

func TestTypedEndpointOwnsBodyCodecFailure(t *testing.T) {
	t.Parallel()
	descriptor := endpointDTO[EndpointDecodedBody](contract.Property{Name: "mode", Type: "text", Required: true})
	e := DefineEndpoint(DefineRoute(RouteSpec{ID: "decode", Method: POST, Access: Public}, StaticPath("/")), EmptyQuery(), JSONBody(descriptor), EmptyResponse(204))
	router, err := NewRouter(e.Handle(func(context.Context, Input[NoPath, NoQuery, EndpointDecodedBody]) (NoContent, error) {
		t.Error("failed body codec reached handler")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"mode":"panic"}`, `{"mode":"goexit"}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 500 || len(decodeFailure(t, w).Issues) != 0 || strings.Contains(w.Body.String(), "private") {
			t.Fatal("body codec failure was not an internal error")
		}
	}
}

func TestTypedEndpointRespectsOuterBodyCeiling(t *testing.T) {
	t.Parallel()
	router, err := NewRouter(patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) {
		t.Error("global body ceiling was bypassed")
		return EndpointReply{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"larger than global limit"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	stdhttp.MaxBytesHandler(router, 8).ServeHTTP(w, r)
	if w.Code != 413 || decodeFailure(t, w).Code != PayloadTooLarge {
		t.Fatal("outer body limit was not retained")
	}
}

func TestTypedEndpointRegistryKeepsMetadataAligned(t *testing.T) {
	t.Parallel()
	typed := patchEndpoint().Handle(func(context.Context, endpointRequest) (EndpointReply, error) { return EndpointReply{}, nil })
	raw := DefineRoute(RouteSpec{ID: "a.raw", Method: GET, Access: Public}, StaticPath("/raw")).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) })
	router, err := NewRouter(typed, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(router.Routes()) != 2 || !router.Routes()[0].Raw || len(router.Endpoints()) != 1 || router.Endpoints()[0].Route.ID != "items.update" {
		t.Fatal("typed metadata included raw payload assumptions")
	}
	if invalid, err := NewRouter(raw, typed, typed); err == nil || invalid != nil {
		t.Fatal("duplicate registry exposed a partial result")
	}
	if len(router.Endpoints()) != 1 {
		t.Fatal("failed registry changed a previous router")
	}
	description, err := patchEndpoint().Description()
	if err != nil {
		t.Fatal(err)
	}
	if description.Route.ID != router.Endpoints()[0].Route.ID || description.Body.Schema.Root != router.Endpoints()[0].Body.Schema.Root {
		t.Fatal("descriptor and registration metadata diverged")
	}
}
