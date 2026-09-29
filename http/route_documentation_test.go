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
	documented := route.WithDocumentation(RouteDocumentation{Summary: "List notes", Tags: tags, Deprecated: true})
	tags[0] = "changed"
	segments, err := documented.validate()
	if err != nil {
		t.Fatal(err)
	}
	info := documented.info(segments, false)
	if info.Documentation == nil || info.Documentation.Tags[0] != "notes" || !info.Documentation.Deprecated {
		t.Fatalf("documentation = %+v", info.Documentation)
	}
	info.Documentation.Tags[0] = "mutated"
	if again := documented.info(segments, false); again.Documentation.Tags[0] != "notes" {
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
		if err := route.WithDocumentation(invalid).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid documentation %+v accepted: %v", invalid, err)
		}
	}
}

func TestEndpointExamplesUseTheDeclaredContracts(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "notes.create", Method: POST, Access: Public}, StaticPath("/notes"))
	endpoint := DefineEndpoint(route, EmptyQuery(), JSONBody(contract.StringJSON[string]()), JSONResponse(201, contract.StringJSON[string]())).WithBodyExample("draft").WithResponseExample("stored")
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	if string(info.Body.Example) != `"draft"` || string(info.Response.Example) != `"stored"` {
		t.Fatalf("examples = %s, %s", info.Body.Example, info.Response.Example)
	}
	empty := DefineEndpoint(route, EmptyQuery(), EmptyBody(), JSONResponse(201, contract.StringJSON[string]())).WithBodyExample(NoBody{})
	if err := empty.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("a body example without a JSON body was accepted", err)
	}
	invalid := DefineEndpoint(route, EmptyQuery(), JSONBody(contract.StringJSON[string]()), JSONResponse(201, contract.StringJSON[string]())).WithBodyExample("bad\xff")
	if err := invalid.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("an example violating its contract was accepted", err)
	}
}
