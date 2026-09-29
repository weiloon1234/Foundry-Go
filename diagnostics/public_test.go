package diagnostics_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

func TestPublicProbesExposeOnlyStatusAndUseApplicationGate(t *testing.T) {
	failing := false
	probes, err := health.NewRegistry(health.DefaultConfig(), health.Probe{ID: "database", Check: func(context.Context) error {
		if failing {
			return errors.New("private dependency credential")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer probes.Close(context.Background())
	gate := &maintenance.Gate{}
	// Observability is not configured: the application gate still drives readiness.
	app, err := foundation.NewBuilder(foundation.WithMaintenance(gate)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	runtime, err := diagnostics.New(app, probes, diagnostics.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(
		diagnostics.PublicProbe(runtime, "probes.live", "/up", diagnostics.Liveness),
		diagnostics.PublicProbe(runtime, "probes.ready", "/ready", diagnostics.Readiness, diagnostics.ReadinessCache(100*time.Millisecond)),
	)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(method, path string) (int, string) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("probe response may be cached")
		}
		return response.Code, response.Body.String()
	}
	if status, body := serve("GET", "/ready"); status != 503 || body != `{"status":"unavailable"}`+"\n" {
		t.Fatal("prepared app reported ready", status, body)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, body string }{{"/up", `{"status":"up"}`}, {"/ready", `{"status":"ready"}`}} {
		if status, body := serve("GET", test.path); status != 200 || body != test.body+"\n" {
			t.Fatal("running app probe failed", test, status, body)
		}
		if status, body := serve("HEAD", test.path); status != 200 || body != "" {
			t.Fatal("HEAD probe wrote a body", test.path, status)
		}
	}
	failing = true
	time.Sleep(150 * time.Millisecond) // The cached answer expires.
	if status, body := serve("GET", "/ready"); status != 503 || strings.Contains(body, "credential") || strings.Contains(body, "database") {
		t.Fatal("public readiness leaked dependency detail", status, body)
	}
	failing = false
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	if status, _ := serve("GET", "/ready"); status != 503 || runtime.Snapshot().Observations.Mode != maintenance.Paused {
		t.Fatal("paused application gate did not fail readiness")
	}
	if status, _ := serve("GET", "/up"); status != 200 {
		t.Fatal("maintenance changed liveness")
	}
	if _, err := foundryhttp.NewRouter(diagnostics.PublicProbe(runtime, "probes.status", "/status", diagnostics.Status)); err == nil {
		t.Fatal("public probe exposed an authenticated endpoint")
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status, body := serve("GET", "/up"); status != 503 || body != `{"status":"down"}`+"\n" {
		t.Fatal("stopped app reported live", status, body)
	}
}

// A flood of public readiness requests shares one dependency check; lifecycle
// and maintenance changes are still reported at once.
func TestPublicReadinessSharesOneCachedCheck(t *testing.T) {
	var checks atomic.Int32
	probes, err := health.NewRegistry(health.DefaultConfig(), health.Probe{ID: "storage", Check: func(ctx context.Context) error {
		checks.Add(1)
		select {
		case <-time.After(50 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer probes.Close(context.Background())
	gate := &maintenance.Gate{}
	app, err := foundation.NewBuilder(foundation.WithMaintenance(gate)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	config := diagnostics.DefaultConfig()
	runtime, err := diagnostics.New(app, probes, config)
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(diagnostics.PublicProbe(runtime, "probes.ready", "/ready", diagnostics.Readiness, diagnostics.ReadinessCache(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	var requests sync.WaitGroup
	var ready atomic.Int32
	for range 64 {
		requests.Go(func() {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/ready", nil))
			if response.Code == 200 {
				ready.Add(1)
			}
		})
	}
	requests.Wait()
	if checks.Load() != 1 || ready.Load() != 64 {
		t.Fatal("public readiness did not share one check", checks.Load(), ready.Load())
	}
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/ready", nil))
	if response.Code != 503 || checks.Load() != 1 {
		t.Fatal("a pause was not reported at once without dependency I/O", response.Code, checks.Load())
	}
	if _, err := foundryhttp.NewRouter(diagnostics.PublicProbe(runtime, "probes.bad", "/bad", diagnostics.Readiness, diagnostics.ReadinessCache(time.Hour))); err == nil {
		t.Fatal("an unbounded readiness cache was accepted")
	}
}
