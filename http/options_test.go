package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	h "github.com/weiloon1234/Foundry-Go/http"
)

func TestEndpointOptionsOwnInputsAndPreserveOrder(t *testing.T) {
	var calls []string
	middleware := func(id h.MiddlewareID) h.Middleware {
		return h.DefineMiddleware(id, func(next http.Handler) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, string(id))
				next.ServeHTTP(w, r)
			}), nil
		})
	}
	chain := []h.Middleware{middleware("first")}
	tags := []string{"original"}
	options := []h.EndpointOption{h.Use(chain...), h.Documentation(h.RouteDocumentation{Tags: tags})}
	chain[0] = middleware("mutated")
	tags[0] = "mutated"
	endpoint := h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "options.order", Method: h.GET, Access: h.Public}, h.StaticPath("/options")), h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204), options...)
	endpoint = h.ConfigureEndpoint(endpoint, h.Use(middleware("second")))
	router, err := h.NewRouter(endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
		calls = append(calls, "handler")
		return h.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/options", nil))
	if response.Code != 204 || !reflect.DeepEqual(calls, []string{"first", "second", "handler"}) {
		t.Fatal(response.Code, calls)
	}
	info, err := h.InspectEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Route.Documentation.Tags, []string{"original"}) {
		t.Fatal(info.Route.Documentation)
	}
	info.Route.Documentation.Tags[0] = "changed snapshot"
	again, err := h.InspectEndpoint(endpoint)
	if err != nil || !reflect.DeepEqual(again.Route.Documentation.Tags, []string{"original"}) {
		t.Fatal(again.Route.Documentation, err)
	}
}
