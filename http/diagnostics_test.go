package http_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func TestDiagnosticsUseNormalPermissionsAndSeparateLiveFromReady(t *testing.T) {
	var loads, checks atomic.Int32
	_, guard, _ := authSetup(t, "bearer", &loads)
	permission := auth.DefinePermission("operations.read", func(_ context.Context, account authAccount) (bool, error) { return account.ID == 1, nil })
	authenticationRegistry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := foundryhttp.NewAuthentication(authenticationRegistry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	probes, err := health.NewRegistry(health.DefaultConfig(), health.Probe{ID: "database", Check: func(context.Context) error { checks.Add(1); return errors.New("private dependency credential") }})
	if err != nil {
		t.Fatal(err)
	}
	defer probes.Close(context.Background())
	app, err := foundation.NewBuilder(foundation.WithObservability(recorder)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	runtime, err := diagnostics.New(app, probes, diagnostics.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := runtime.Readiness(t.Context()); err != nil || ready.Ready || checks.Load() != 0 {
		t.Fatal("prepared app checked dependencies or reported ready", err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	base := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "operations", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/profile"))
	protected := foundryhttp.RequireRouteAuthentication(base, authentication, guard).WithPermissions(permission)
	for _, endpoint := range []diagnostics.Endpoint{diagnostics.Liveness, diagnostics.Readiness, diagnostics.Status, diagnostics.Metrics} {
		router := newAuthRouter(t, diagnostics.Route(runtime, protected, endpoint))
		before := checks.Load()
		for _, test := range []struct {
			headers []string
			status  int
		}{{nil, 401}, {[]string{"Bearer other"}, 403}} {
			response := authServe(router, test.headers)
			if response.Code != test.status || checks.Load() != before {
				t.Fatal("diagnostics bypassed normal authentication/permissions", response.Code)
			}
		}
		response := authServe(router, []string{"Bearer valid"})
		want := 200
		if endpoint == diagnostics.Readiness {
			want = 503
		}
		if response.Code != want || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private dependency credential") {
			t.Fatal("incorrect operational response", response.Code, response.Body.String())
		}
		if endpoint == diagnostics.Liveness && checks.Load() != before {
			t.Fatal("dependency failure affected liveness")
		}
		request := httptest.NewRequest("HEAD", "/profile", nil)
		request.Header.Set("Authorization", "Bearer valid")
		head := httptest.NewRecorder()
		router.ServeHTTP(head, request)
		if head.Code != want || head.Body.Len() != 0 {
			t.Fatal("diagnostics HEAD wrote a body or lost status")
		}
	}
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	before := checks.Load()
	if ready, err := runtime.Readiness(t.Context()); err != nil || ready.Ready || checks.Load() != before || !runtime.Liveness().Live {
		t.Fatal("maintenance/liveness/readiness boundary failed", err)
	}
	// Exercise the actual listener's maintenance exception through the same
	// protected descriptor: bypassing maintenance must not bypass authorization.
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	path, err := protected.URL(foundryhttp.NoPath{})
	if err != nil {
		t.Fatal(err)
	}
	config.MaintenanceReadPaths = []string{path}
	server, err := foundryhttp.Prepare(newAuthRouter(t, diagnostics.Route(runtime, protected, diagnostics.Liveness)), config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	serveContext, stop := context.WithCancel(observability.WithContext(t.Context(), recorder))
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serveContext) }()
	defer func() {
		stop()
		select {
		case err := <-serverDone:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("protected diagnostics server did not stop")
		}
	}()
	readyContext, readyCancel := context.WithTimeout(t.Context(), 3*time.Second)
	address, err := server.Ready(readyContext)
	readyCancel()
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, test := range []struct {
		method, path, token string
		status              int
	}{
		{"GET", path, "", 401}, {"GET", path, "Bearer other", 403},
		{"GET", path, "Bearer valid", 200}, {"HEAD", path, "Bearer valid", 200},
		{"POST", path, "Bearer valid", 503}, {"GET", path + "/extra", "Bearer valid", 503},
		{"GET", "/%70rofile", "Bearer valid", 503},
	} {
		request, err := stdhttp.NewRequestWithContext(t.Context(), test.method, "http://"+address+test.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", test.token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != test.status {
			t.Fatal("native maintenance/auth boundary failed", test, response.StatusCode, readErr, closeErr)
		}
	}
	if checks.Load() != before {
		t.Fatal("liveness or rejected maintenance requests ran dependency probes")
	}
}
