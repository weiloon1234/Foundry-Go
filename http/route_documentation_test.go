package http

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRouteDocumentationIsValidatedAndOwned(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "notes.list", Method: GET, Access: Public}, StaticPath("/notes"))
	tags := []string{"notes"}
	documented := ConfigureRoute(route, Documentation(RouteDocumentation{Summary: "List notes", Tags: tags, Deprecated: true}))
	tags[0] = "changed"
	segments, err := routeValidate(documented)
	if err != nil {
		t.Fatal(err)
	}
	info := routeInfo(documented, segments, false)
	if info.Documentation == nil || info.Documentation.Tags[0] != "notes" || !info.Documentation.Deprecated {
		t.Fatalf("documentation = %+v", info.Documentation)
	}
	info.Documentation.Tags[0] = "mutated"
	if again := routeInfo(documented, segments, false); again.Documentation.Tags[0] != "notes" {
		t.Fatal("route info shares documentation tags")
	}
	for _, invalid := range []RouteDocumentation{
		{Summary: "two\nlines"},
		{Summary: strings.Repeat("s", MaxRouteSummaryBytes+1)},
		{Description: "bell\a"},
		{Tags: []string{"a", "a"}},
		{Tags: []string{" padded"}},
		{Tags: []string{""}},
	} {
		if err := ValidateRoute(ConfigureRoute(route, Documentation(invalid))); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid documentation %+v accepted: %v", invalid, err)
		}
	}
}

func TestEndpointExamplesUseTheDeclaredContracts(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "notes.create", Method: POST, Access: Public}, StaticPath("/notes"))
	endpoint := ResponseExample(BodyExample(DefineEndpoint(route, EmptyQuery(), JSONBody(contract.StringJSON[string]()), JSONResponse(201, contract.StringJSON[string]())), "draft"), "stored")
	info, err := InspectEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if string(info.Body.Example) != `"draft"` || string(info.Response.Example) != `"stored"` {
		t.Fatalf("examples = %s, %s", info.Body.Example, info.Response.Example)
	}
	empty := BodyExample(DefineEndpoint(route, EmptyQuery(), EmptyBody(), JSONResponse(201, contract.StringJSON[string]())), NoBody{})
	if err := ValidateEndpoint(empty); !errors.Is(err, fault.Invalid) {
		t.Fatal("a body example without a JSON body was accepted", err)
	}
	invalid := BodyExample(DefineEndpoint(route, EmptyQuery(), JSONBody(contract.StringJSON[string]()), JSONResponse(201, contract.StringJSON[string]())), "bad\xff")
	if err := ValidateEndpoint(invalid); !errors.Is(err, fault.Invalid) {
		t.Fatal("an example violating its contract was accepted", err)
	}
}
