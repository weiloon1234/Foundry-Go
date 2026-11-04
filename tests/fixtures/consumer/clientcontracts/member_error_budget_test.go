package clientcontracts_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"foundry.test/consumer/clientcontracts"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

// The native gate compiles each client from exactly its server's limits. These
// are temporary contracts, independent of the published cohort.
func memberErrorBudgetClients(t *testing.T, dir string) http.Handler {
	t.Helper()
	handlers := make(map[string]http.Handler)
	for steps := 15; steps <= 40; steps++ {
		limits := foundryhttp.DefaultEndpointLimits()
		limits.Response.Steps = steps
		endpoint := foundryhttp.ConfigureEndpoint(clientcontracts.MemberCreate, foundryhttp.Limits(limits))
		router, err := foundryhttp.NewRouter(endpoint.Handle(clientcontracts.HandleMemberCreate))
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
