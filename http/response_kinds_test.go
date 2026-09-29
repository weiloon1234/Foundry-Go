package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func upsertEndpoint() Endpoint[userPath, endpointParameters, EndpointPatch, Statused[EndpointReply]] {
	route := DefineRoute(RouteSpec{ID: "items.upsert", Method: PUT, Access: Public}, DefinePath("/items/{user}", Param("user", ModelIDPath[routeUser](), func(p *userPath) *model.ID[routeUser] { return &p.User })))
	query := DefineQuery(OptionalQueryParam("q", StringQuery[string](), func(q *endpointParameters) *value.Optional[string] { return &q.Term }))
	return DefineEndpoint(route, query, JSONBody(endpointPatchJSON()), JSONResponses(endpointReplyJSON(), 200, 201))
}

// A handler selects one of the declared success statuses; an undeclared status
// is an internal failure and nothing is published.
func TestJSONResponsesPublishTheSelectedDeclaredStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		selected int
		want     int
	}{{"primary", 0, 200}, {"created", 201, 201}, {"explicit-primary", 200, 200}, {"undeclared", 202, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			router, err := NewRouter(upsertEndpoint().Handle(func(_ context.Context, in Input[userPath, endpointParameters, EndpointPatch]) (Statused[EndpointReply], error) {
				return Statused[EndpointReply]{Status: tc.selected, Value: EndpointReply{ID: in.Path.User.String(), Name: in.Body.Name}}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("PUT", "/items/"+endpointUserID, strings.NewReader(`{"name":"Jane"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			if tc.want != 500 && !strings.Contains(response.Body.String(), `"name":"Jane"`) {
				t.Fatal("selected value was not encoded", response.Body.String())
			}
		})
	}
	info, err := upsertEndpoint().Description()
	if err != nil || info.Status != 200 || len(info.Statuses) != 2 || info.Statuses[1] != 201 || info.Response == nil {
		t.Fatal("status metadata", info.Statuses, err)
	}
	for _, statuses := range [][]int{{200}, {200, 200}, {200, 204}, {200, 302}, {200, 201, 202, 203, 206, 207, 208, 226, 299}} {
		if err := JSONResponses(endpointReplyJSON(), statuses[0], statuses[1:]...).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid status set accepted", statuses)
		}
	}
	if err := upsertEndpoint().Idempotent(nil, idempotency.Definition{}).Validate(); err == nil {
		t.Fatal("idempotent endpoint accepted several statuses")
	}
}

func redirectEndpoint() Endpoint[NoPath, NoQuery, NoBody, Redirect] {
	return DefineEndpoint(DefineRoute(RouteSpec{ID: "session.continue", Method: POST, Access: Public}, StaticPath("/continue")), EmptyQuery(), EmptyBody(), RedirectResponse(303))
}

// Redirects carry a validated relative Location and no body; request input can
// never select another host.
func TestRedirectResponsesAcceptOnlyRelativeTargets(t *testing.T) {
	t.Parallel()
	account := DefineRoute(RouteSpec{ID: "account.show", Method: GET, Access: Public}, StaticPath("/account"))
	for _, tc := range []struct {
		name     string
		target   Redirect
		status   int
		location string
	}{
		{"route", RedirectToRoute(account, NoPath{}), 303, "/account"},
		{"relative", RedirectTo("/orders?page=2#top"), 303, "/orders?page=2#top"},
		{"scheme-relative", RedirectTo("//evil.test/"), 500, ""},
		{"backslash", RedirectTo("/\\evil.test/"), 500, ""},
		{"absolute", RedirectTo("https://evil.test/"), 500, ""},
		{"relative-path", RedirectTo("orders"), 500, ""},
		{"control", RedirectTo("/a\r\nSet-Cookie: x=1"), 500, ""},
		{"space", RedirectTo("/a b"), 500, ""},
		{"zero", Redirect{}, 500, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, err := NewRouter(redirectEndpoint().Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Redirect, error) {
				return tc.target, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/continue", nil))
			if response.Code != tc.status || response.Header().Get("Location") != tc.location {
				t.Fatalf("%d %q %s", response.Code, response.Header().Get("Location"), response.Body.String())
			}
			if tc.status == 303 && (response.Body.Len() != 0 || response.Header().Get("Content-Length") != "0") {
				t.Fatal("redirect has a body", response.Body.String())
			}
		})
	}
	info, err := redirectEndpoint().Description()
	if err != nil || !info.Redirect || info.Status != 303 || info.Response != nil {
		t.Fatal("redirect metadata", info, err)
	}
	for _, status := range []int{200, 300, 304, 305, 399} {
		if err := RedirectResponse(status).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("non-redirect status accepted", status)
		}
	}
	if _, err := json.Marshal(RedirectTo("/a")); err == nil {
		t.Fatal("redirect was implicitly serialized")
	}
	if location, err := RedirectTo("/a").Location(); err != nil || location != "/a" {
		t.Fatal("location", location, err)
	}
}

func rawEndpoint(limit int64) Endpoint[NoPath, NoQuery, RawBody, EndpointReply] {
	limits := DefaultEndpointLimits()
	limits.Raw.Bytes = limit
	route := DefineRoute(RouteSpec{ID: "files.raw", Method: POST, Access: Public}, StaticPath("/raw"))
	return DefineEndpoint(route, EmptyQuery(), RawRequestBody("application/octet-stream", "text/plain"), JSONResponse(201, endpointReplyJSON())).WithLimits(limits)
}

// A raw body streams to the handler with declared media and bounded reads.
func TestRawRequestBodyStreamsBoundedDeclaredMedia(t *testing.T) {
	t.Parallel()
	var retained RawBody
	router, err := NewRouter(rawEndpoint(16).Handle(func(_ context.Context, in Input[NoPath, NoQuery, RawBody]) (EndpointReply, error) {
		retained = in.Body
		data, err := io.ReadAll(in.Body)
		if err != nil {
			return EndpointReply{}, err
		}
		length, _ := in.Body.Length().Get()
		return EndpointReply{Name: string(data), Query: string(in.Body.MediaType()), Null: length == int64(len(data))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, media string
		body        io.Reader
		chunked     bool
		status      int
	}{
		{"octets", "application/octet-stream", strings.NewReader("raw-bytes"), false, 201},
		{"text-with-charset", "text/plain; charset=utf-8", strings.NewReader("text"), false, 201},
		{"chunked", "application/octet-stream", strings.NewReader("streamed"), true, 201},
		{"undeclared-media", "image/png", strings.NewReader("x"), false, 415},
		{"declared-too-large", "application/octet-stream", strings.NewReader(strings.Repeat("x", 17)), false, 413},
		{"streamed-too-large", "application/octet-stream", strings.NewReader(strings.Repeat("x", 17)), true, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/raw", tc.body)
			if tc.chunked {
				request.Body = io.NopCloser(tc.body)
				request.ContentLength = -1
			}
			request.Header.Set("Content-Type", tc.media)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			if tc.status == 201 {
				var reply EndpointReply
				if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || reply.Query != tc.media || reply.Null == tc.chunked {
					t.Fatal("raw body reply", response.Body.String(), err)
				}
			}
		})
	}
	if _, err := retained.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("raw body was readable after the handler returned", err)
	}
	info, err := rawEndpoint(16).Description()
	if err != nil || info.Body == nil || info.Body.Raw == nil || len(info.Body.Raw.MediaTypes) != 2 || info.Body.MediaType != "" || info.Limits.Raw.Bytes != 16 {
		t.Fatal("raw metadata", info.Body, err)
	}
	if err := rawEndpoint(0).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("raw endpoint without a raw limit accepted", err)
	}
	if err := RawRequestBody("text/plain", "text/plain; charset=utf-8").Validate(); err == nil {
		t.Fatal("repeated raw media accepted")
	}
	get := DefineEndpoint(DefineRoute(RouteSpec{ID: "files.get", Method: GET, Access: Public}, StaticPath("/get")), EmptyQuery(), RawRequestBody("application/octet-stream"), JSONResponse(200, endpointReplyJSON()))
	if err := get.Validate(); err == nil {
		t.Fatal("GET accepted a raw body")
	}
	if _, err := json.Marshal(RawBody{}); err == nil {
		t.Fatal("raw body was implicitly serialized")
	}
}

// Raw bodies are opaque to contract descriptors and never enter JSON schemas.
func TestRawBodyMetadataHasNoSchema(t *testing.T) {
	t.Parallel()
	info, err := rawEndpoint(8).Description()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info.Body)
	if err != nil || !bytes.Contains(data, []byte(`"raw":{"media_types":["application/octet-stream","text/plain"]}`)) || info.Body.Schema.Root != contract.TypeID("") {
		t.Fatal("raw body metadata", string(data), err)
	}
}
