package attribution_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRouteIsValidatedRequestLocalMetadata(t *testing.T) {
	if _, present := attribution.RouteFromContext(t.Context()); present {
		t.Fatal("route fabricated outside a request")
	}
	route := attribution.Route{Method: "POST", Name: "accounts.update"}
	ctx, err := attribution.WithRoute(t.Context(), route)
	if err != nil {
		t.Fatal(err)
	}
	if seen, present := attribution.RouteFromContext(ctx); !present || seen != route {
		t.Fatal("route was not retained")
	}
	for _, invalid := range []attribution.Route{{}, {Method: "post", Name: "a"}, {Method: "POST"}, {Method: "POST", Name: "has space"},
		{Method: "POST", Name: strings.Repeat("x", attribution.MaxRouteNameBytes+1)}, {Method: strings.Repeat("A", attribution.MaxRouteMethodBytes+1), Name: "a"}} {
		if _, err := attribution.WithRoute(t.Context(), invalid); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid route accepted", err)
		}
	}
	origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(attribution.FromContext(ctx))
	if err != nil || strings.Contains(string(encoded), "accounts.update") {
		t.Fatal("route leaked into serialized origin", err)
	}
}
