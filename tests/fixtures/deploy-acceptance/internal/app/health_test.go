package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/http"
)

func TestDeployedHealthWireContractAndRealReadiness(t *testing.T) {
	settings := fixtureconfig.Defaults()
	receipts := work.NewReceipts(t.TempDir())
	mode := "healthy"
	built, err := application.New(settings.App).HTTP(healthRoutes).
		Features(func(application.Services) (application.FeatureDeclarations, error) {
			return application.FeatureDeclarations{Readiness: []health.Probe{{ID: "acceptance.readiness", Check: func(ctx context.Context) error {
				return readiness(ctx, mode, receipts)
			}}}}, nil
		}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := shutdown(built); err != nil {
			t.Error(err)
		}
	})
	routes, err := healthRoutes(built.Resources())
	if err != nil {
		t.Fatal(err)
	}
	router, err := http.NewRouter(routes...)
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string, status int, want string) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: status %d, headers %v", path, response.Code, response.Header())
		}
		if want != "" {
			var body, expected map[string]any
			if json.Unmarshal(response.Body.Bytes(), &body) != nil || json.Unmarshal([]byte(want), &expected) != nil {
				t.Fatal("invalid probe JSON")
			}
			if len(body) != len(expected) {
				t.Fatal("probe exposes additional fields")
			}
			for key, value := range expected {
				if body[key] != value {
					t.Fatal("unexpected probe body", response.Body.String())
				}
			}
		} else if strings.Contains(response.Body.String(), "fixture dependency") {
			t.Fatal("readiness leaked dependency details")
		}
	}
	check("/health/live", 200, `{"status":"ok"}`)
	check("/health/ready", 503, "")
	if err := built.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	check("/health/ready", 200, `{"ready":true}`)
	mode = "fail"
	check("/health/ready", 503, "")
	check("/health/live", 200, `{"status":"ok"}`)
	mode = "hold"
	check("/health/ready", 503, "")
	if err := receipts.Mark("readiness.release"); err != nil {
		t.Fatal(err)
	}
	check("/health/ready", 200, `{"ready":true}`)
}
