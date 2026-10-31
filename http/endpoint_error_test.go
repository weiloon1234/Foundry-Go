package http_test

import (
	"errors"
	"strings"
	"testing"

	h "github.com/weiloon1234/Foundry-Go/http"
)

func TestInvalidEndpointOptionPreservesStickyCause(t *testing.T) {
	route := h.DefineRoute(h.RouteSpec{ID: "adapter.test", Method: h.GET, Access: h.Public}, h.StaticPath("/adapter"))
	endpoint := h.DefineEndpoint(route, h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204))
	cause := errors.New("private construction details")
	invalid := h.ConfigureEndpoint(endpoint, h.InvalidEndpointOption(cause))
	err := h.ValidateEndpoint(invalid)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "adapter.test") || strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("construction cause was lost or exposed: %v", err)
	}
	later := h.ConfigureEndpoint(invalid, h.InvalidEndpointOption(errors.New("later")), h.InvalidEndpointOption(nil))
	if h.ValidateEndpoint(later) != err {
		t.Fatal("earlier declaration failure replaced")
	}
	if h.ValidateEndpoint(h.ConfigureEndpoint(endpoint, h.InvalidEndpointOption(nil))) == nil {
		t.Fatal("nil cause accepted")
	}
	if h.ValidateEndpoint(endpoint) != nil {
		t.Fatal("original endpoint mutated")
	}
}
