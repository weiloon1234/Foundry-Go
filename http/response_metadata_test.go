package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Typed endpoints derive allowlisted response headers from their result.
func TestEndpointResponseHeadersFromResult(t *testing.T) {
	t.Parallel()
	show := DefineRoute(RouteSpec{ID: "items.show", Method: GET, Access: Public}, DefinePath("/items/{user}", Param("user", ModelIDPath[routeUser](), func(p *userPath) *model.ID[routeUser] { return &p.User })))
	for _, tc := range []struct {
		name    string
		headers func(EndpointReply) ([]ResponseHeader, error)
		status  int
	}{
		{"allowed", func(reply EndpointReply) ([]ResponseHeader, error) {
			id, err := model.ParseID[routeUser](reply.ID)
			if err != nil {
				return nil, err
			}
			location, err := RouteLocation(show, userPath{User: id})
			return []ResponseHeader{location, {"Cache-Control", "private, max-age=60"}, {"Link", "</a>; rel=next"}, {"Link", "</b>; rel=prev"}, {"Vary", "Accept, Origin"}, {"x-trace-note", "kept"}}, err
		}, 201},
		{"reserved", func(EndpointReply) ([]ResponseHeader, error) {
			return []ResponseHeader{{"X-Accel-Redirect", "/internal/secret"}}, nil
		}, 500},
		{"framing", func(EndpointReply) ([]ResponseHeader, error) {
			return []ResponseHeader{{"Content-Type", "text/html"}}, nil
		}, 500},
		{"duplicate", func(EndpointReply) ([]ResponseHeader, error) {
			return []ResponseHeader{{"Location", "/a"}, {"Location", "/b"}}, nil
		}, 500},
		{"control", func(EndpointReply) ([]ResponseHeader, error) {
			return []ResponseHeader{{"Location", "/a\r\nSet-Cookie: x=1"}}, nil
		}, 500},
		{"error", func(EndpointReply) ([]ResponseHeader, error) {
			return nil, errors.New("private header failure")
		}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := patchEndpoint().WithHeaders(func(_ context.Context, reply EndpointReply) ([]ResponseHeader, error) { return tc.headers(reply) })
			router, err := NewRouter(endpoint.Handle(func(_ context.Context, in endpointRequest) (EndpointReply, error) {
				return EndpointReply{ID: in.Path.User.String(), Name: in.Body.Name}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("PATCH", "/items/"+endpointUserID, strings.NewReader(`{"name":"Jane"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			response.Header().Set("Vary", "Accept-Encoding")
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			header := response.Header()
			if tc.status != 201 {
				if header.Get("Location") != "" || header.Get("X-Accel-Redirect") != "" || strings.Contains(response.Body.String(), "private") {
					t.Fatal("rejected header or cause escaped", header)
				}
				return
			}
			if header.Get("Location") != "/items/"+endpointUserID || header.Get("Cache-Control") != "private, max-age=60" || len(header.Values("Link")) != 2 || header.Get("X-Trace-Note") != "kept" || header.Get("Content-Type") != "application/json" {
				t.Fatal("allowed headers", header)
			}
			if vary := strings.Join(header.Values("Vary"), ","); vary != "Accept-Encoding,Accept,Origin" {
				t.Fatal("Vary replaced instead of extended", vary)
			}
		})
	}
}

func TestEndpointResponseHeadersValidation(t *testing.T) {
	t.Parallel()
	if err := patchEndpoint().WithHeaders(nil).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil header callback accepted", err)
	}
	download := DefineEndpoint(DefineRoute(RouteSpec{ID: "report", Method: GET, Access: Public}, StaticPath("/report")), EmptyQuery(), EmptyBody(), DownloadResponse("text/plain"))
	if err := download.WithHeaders(func(context.Context, Download) ([]ResponseHeader, error) { return nil, nil }).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("file response headers accepted", err)
	}
	endpoint := patchEndpoint().WithHeaders(func(context.Context, EndpointReply) ([]ResponseHeader, error) { return nil, nil })
	if err := endpoint.Idempotent(nil, idempotency.Definition{}).Validate(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "IdempotentEndpoint.WithHeaders") {
		t.Fatal("idempotent endpoint accepted non-replayable headers", err)
	}
	if _, err := EndpointLocation(t.Context(), patchEndpoint(), userPath{}, endpointParameters{}); err == nil {
		t.Fatal("location for an invalid path")
	}
}
