package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// sharedMaintenance stores fleet state in a file cache shared by sequential
// and concurrent applications, standing in for a Redis/PostgreSQL store.
func sharedMaintenance(t *testing.T, root string) application.Settings {
	s := settings()
	store := infrastructure.DefaultCacheSettings()
	store.Driver = infrastructure.FileCache
	store.File.Root = root
	s.Services.Cache.Stores["shared"] = store
	s.Maintenance.Store = "shared"
	s.Maintenance.PollInterval = 100 * time.Millisecond
	return s
}

func runCommand(t *testing.T, s application.Settings, args ...string) string {
	t.Helper()
	declarations, err := application.MaintenanceCommands()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	invocation, err := registry.Parse(args, &output)
	if err != nil {
		t.Fatal(err)
	}
	s.HTTP.Enabled = false
	commands := cli.Module("fixture.cli", foundation.NewKey[*cli.Registry]("fixture.commands"), registry, invocation, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}, application.MaintenanceProvider)
	app, err := application.New(s, quiet()).Register(commands).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(t.Context(), foundation.CLI); err != nil {
		t.Fatal(err, output.String())
	}
	return output.String()
}

func TestMaintenanceIsFleetWideWithoutObservabilityAndKeepsProbesReachable(t *testing.T) {
	root := t.TempDir()
	output := runCommand(t, sharedMaintenance(t, root), "down", "--message", "Upgrading", "--retry", "30", "--with-secret", "--except", "POST /hooks/*")
	secretLine := strings.TrimSpace(output[strings.Index(output, "/")+1:])
	secretValue := strings.Fields(secretLine)[0]
	if !strings.Contains(output, "Maintenance mode enabled.") || len(secretValue) < 16 {
		t.Fatal("down command did not report its bypass", output)
	}
	s := sharedMaintenance(t, root)
	s.HTTP.Probes.Liveness, s.HTTP.Probes.Readiness = true, true
	if s.Features.Observability.Enabled {
		t.Fatal("fixture unexpectedly enabled observability")
	}
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	gate, err := app.Resources().Maintenance()
	if err != nil || gate != app.Maintenance() {
		t.Fatal("services did not expose the application gate", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	ready, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
	address, err := app.HTTPReady(ready)
	readyCancel()
	if err != nil {
		t.Fatal(err)
	}
	client := &stdhttp.Client{Timeout: 3 * time.Second, CheckRedirect: func(*stdhttp.Request, []*stdhttp.Request) error { return stdhttp.ErrUseLastResponse }}
	get := func(path string) (*stdhttp.Response, string) {
		t.Helper()
		response, err := client.Get("http://" + address + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response, string(body)
	}
	response, body := get("/ok")
	var payload http.ErrorResponse
	if response.StatusCode != 503 || response.Header.Get("Retry-After") != "30" || json.Unmarshal([]byte(body), &payload) != nil || payload.Message != "Upgrading" {
		t.Fatal("fleet maintenance was not applied at boot", response.StatusCode, body)
	}
	if response, body := get("/up"); response.StatusCode != 200 || body != `{"status":"up"}`+"\n" {
		t.Fatal("liveness was blocked by maintenance", response.StatusCode, body)
	}
	if response, _ := get("/ready"); response.StatusCode != 503 {
		t.Fatal("paused application reported ready")
	}
	if response, _ := get("/" + secretValue); response.StatusCode != stdhttp.StatusSeeOther {
		t.Fatal("shared bypass secret was not accepted", response.StatusCode)
	}
	runCommand(t, sharedMaintenance(t, root), "up")
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, _ := get("/ok")
		if response.StatusCode == 204 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("instance did not observe the fleet resume", response.StatusCode)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if response, body := get("/ready"); response.StatusCode != 200 || body != `{"status":"ready"}`+"\n" {
		t.Fatal("resumed application was not ready", response.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HTTP kernel did not stop")
	}
}

func TestMaintenanceConfigurationValidation(t *testing.T) {
	missing := settings()
	missing.Maintenance.Store = "absent"
	if _, err := application.New(missing, quiet()).Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("unknown maintenance store accepted", err)
	}
	discarding := settings()
	null := infrastructure.DefaultCacheSettings()
	null.Driver = infrastructure.NullCache
	discarding.Services.Cache.Stores["discarded"] = null
	discarding.Maintenance.Store = "discarded"
	if _, err := application.New(discarding, quiet()).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("null maintenance store accepted", err)
	}
	probe := settings()
	probe.HTTP.Probes.Liveness, probe.HTTP.Probes.LivenessPath = true, "up"
	if _, err := application.New(probe, quiet()).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid probe path accepted", err)
	}
	probe.HTTP.Probes.LivenessPath, probe.HTTP.Probes.Readiness, probe.HTTP.Probes.ReadinessPath = "/health", true, "/health"
	if _, err := application.New(probe, quiet()).Build(t.Context()); err == nil {
		t.Fatal("duplicate probe paths accepted")
	}
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	supplied := settings()
	supplied.Maintenance.Exempt = []maintenance.Rule{{Path: "/status"}}
	if _, err := application.New(supplied, quiet(), application.WithObservability(recorder)).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("supplied recorder gate accepted a second policy", err)
	}
	app, err := application.New(settings(), quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop(t, app)
	if _, err := app.Resources().MaintenanceStore(); err == nil {
		t.Fatal("local maintenance reported a shared store")
	}
}

func TestAboutDescribesDriversWithoutSecrets(t *testing.T) {
	s := settings()
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary.Host, connection.Primary.Password = "db.private.example", secret.New("private-password")
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"main": connection}
	declaration, err := application.AboutCommand("about", s)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"about"}, {"about", "--json"}} {
		var output strings.Builder
		invocation, err := registry.Parse(args, &output)
		if err != nil {
			t.Fatal(err)
		}
		if err := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}); err != nil {
			t.Fatal(err)
		}
		text := output.String()
		if !strings.Contains(text, "postgres") || !strings.Contains(text, "memory") || !strings.Contains(text, "go1.") || strings.Contains(text, "private") {
			t.Fatal("about output omitted drivers or leaked configuration", text)
		}
		if len(args) == 2 {
			var about application.About
			if err := json.Unmarshal([]byte(text), &about); err != nil || about.Kernels["http"] != s.HTTP.Server.Address || about.Services["database main"] != "postgres" {
				t.Fatal("about JSON changed", err, text)
			}
		}
	}
}

// Configured assembly hands its global TrustedProxy to kernel admission, so a
// maintenance allow list naming the balancer's subnet does not admit the public.
func TestConfiguredMaintenanceAllowsMatchTheTrustedProxyClient(t *testing.T) {
	s := settings()
	s.Maintenance.Allow = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	proxy := http.TrustedProxy(http.TrustedProxyConfig{Proxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Headers: []http.ProxyHeader{http.XForwardedForHeader()}})
	app, err := application.New(s, quiet()).Use(proxy).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	ready, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
	address, err := app.HTTPReady(ready)
	readyCancel()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Maintenance().Set(true); err != nil {
		t.Fatal(err)
	}
	status := func(forwardedFor string) int {
		t.Helper()
		request, err := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodGet, "http://"+address+"/ok", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Forwarded-For", forwardedFor)
		response, err := (&stdhttp.Client{Timeout: 3 * time.Second}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}
	if status("203.0.113.9") != stdhttp.StatusServiceUnavailable || status("127.0.0.7") != stdhttp.StatusNoContent {
		t.Fatal("maintenance allow list matched the balancer instead of the client")
	}
	cancel()
	<-done
}
