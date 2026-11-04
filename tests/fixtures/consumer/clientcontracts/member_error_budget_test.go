package clientcontracts_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"foundry.test/consumer/clientcontracts"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/value"
)

type BudgetChoice struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type UnionBudgetDetails struct {
	Choice BudgetChoice `json:"choice"`
}

type CollectionBudgetDetails struct {
	Choice BudgetChoice            `json:"choice"`
	Array  []BudgetChoice          `json:"array"`
	Map    map[string]BudgetChoice `json:"map"`
	Note   value.Optional[string]  `json:"note,omitzero"`
}

// The native gate compiles each client from exactly its server's limits. These
// are temporary contracts, independent of the published cohort.
func memberErrorBudgetClients(t *testing.T, dir string) http.Handler {
	t.Helper()
	const unionRoot = "foundry.test/consumer/clientcontracts_test.UnionBudgetDetails"
	const collectionRoot = "foundry.test/consumer/clientcontracts_test.CollectionBudgetDetails"
	types := []contract.Type{
		{ID: "budget.Choice", Kind: contract.UnionKind, Discriminator: "kind", Variants: []contract.Variant{{Tag: "existing", Type: "budget.Payload"}}},
		{ID: "budget.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "label", Type: "string", Required: true}}},
		{ID: "budget.Array", Kind: contract.ArrayKind, Element: "budget.Choice"},
		contract.JSONMapType[map[string]BudgetChoice]("budget.Map", "budget.Choice", "string", contract.StringJSONKey[string]()),
		{ID: "string", Kind: contract.StringKind},
	}
	choice := contract.Property{Name: "choice", Type: "budget.Choice", Required: true}
	union := foundryhttp.DefineErrorDetails("union_budget", 409, "Conflict", contract.DefineJSON[UnionBudgetDetails](contract.Schema{Root: unionRoot, Types: append(append([]contract.Type(nil), types...), contract.Type{ID: unionRoot, Kind: contract.ObjectKind, Properties: []contract.Property{choice}})}))
	collection := foundryhttp.DefineErrorDetails("collection_budget", 409, "Conflict", contract.DefineJSON[CollectionBudgetDetails](contract.Schema{Root: collectionRoot, Types: append(append([]contract.Type(nil), types...), contract.Type{ID: collectionRoot, Kind: contract.ObjectKind, Properties: []contract.Property{
		choice, {Name: "array", Type: "budget.Array", Required: true}, {Name: "map", Type: "budget.Map", Required: true}, {Name: "note", Type: "string"},
	}})}))
	payload := CollectionBudgetDetails{Choice: BudgetChoice{Kind: "existing", Label: "x"}, Array: []BudgetChoice{{Kind: "existing", Label: "a"}}, Map: map[string]BudgetChoice{"first": {Kind: "existing", Label: "m"}}}
	handlers := make(map[string]http.Handler)
	for steps := 15; steps <= 48; steps++ {
		limits := foundryhttp.DefaultEndpointLimits()
		limits.Response.Steps = steps
		endpoint := foundryhttp.ConfigureEndpoint(clientcontracts.MemberCreate, foundryhttp.Limits(limits))
		private := func(id foundryhttp.RouteID, path string, declaration foundryhttp.ErrorDeclarationOption, failure error) foundryhttp.RouteRegistration {
			endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204), foundryhttp.Errors(declaration))
			return foundryhttp.ConfigureEndpoint(endpoint, foundryhttp.Limits(limits)).Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
				return foundryhttp.NoContent{}, failure
			})
		}
		optional := payload
		optional.Note = value.Set("note")
		router, err := foundryhttp.NewRouter(endpoint.Handle(clientcontracts.HandleMemberCreate),
			private("budget.union", "/union", union, union.New(UnionBudgetDetails{Choice: payload.Choice})),
			private("budget.collections", "/collections", collection, collection.New(payload)),
			private("budget.optional", "/optional", collection, collection.New(optional)),
		)
		if err != nil {
			t.Fatal(steps, err)
		}
		source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
		if err != nil {
			t.Fatal(err)
		}
		output, err := typescript.Render(source)
		if err != nil {
			t.Fatal(err)
		}
		name := strconv.Itoa(steps)
		if err := os.WriteFile(filepath.Join(dir, "steps-"+name+".ts"), output, 0600); err != nil {
			t.Fatal(err)
		}
		handlers[name] = router
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := handlers[r.Header.Get("X-Fixture-Steps")]
		if handler == nil {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/members" {
			handler.ServeHTTP(w, r)
			return
		}
		origin, err := (attribution.Origin{}).WithRequest(attribution.Request{ID: "11111111-1111-4111-8111-111111111111"})
		if err != nil {
			t.Error(err)
			http.Error(w, "request metadata", 500)
			return
		}
		ctx, err := attribution.WithContext(r.Context(), origin)
		if err != nil {
			t.Error(err)
			http.Error(w, "request metadata", 500)
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestMemberErrorResponseDepthFloor(t *testing.T) {
	for _, depth := range []int{2, 3} {
		limits := foundryhttp.DefaultEndpointLimits()
		limits.Response.Depth = depth
		endpoint := foundryhttp.ConfigureEndpoint(clientcontracts.MemberCreate, foundryhttp.Limits(limits))
		err := foundryhttp.ValidateEndpoint(endpoint)
		if (err == nil) != (depth == 3) {
			t.Fatal("required nested details depth", depth, err)
		}
	}
}
