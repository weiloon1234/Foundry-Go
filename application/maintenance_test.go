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

func TestMaintenanceCommandFormats(t *testing.T) {
	for _, format := range []string{"", "text", "json"} {
		t.Run(format, func(t *testing.T) {
			s := sharedMaintenance(t, t.TempDir())
			for _, action := range []string{"down", "up", "maintenance:status"} {
				args := []string{action}
				if format != "" {
					args = append(args, "--format", format)
				}
				want := "Maintenance mode enabled.\n"
				if action == "up" {
					want = "Maintenance mode disabled.\n"
				}
				if action == "maintenance:status" {
					want = "Application is up\n"
				}
				if format == "json" {
					want = "{\"down\":true}\n"
					if action != "down" {
						want = "{\"down\":false}\n"
					}
				}
				if output := runCommand(t, s, args...); output != want {
					t.Fatalf("%s output = %q, want %q", action, output, want)
				}
			}
		})
	}
}

func TestDownOnlyPrintsGeneratedSecret(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, generated := range []bool{false, true} {
			s := sharedMaintenance(t, t.TempDir())
			args := []string{"down", "--format", format}
			if generated {
				args = append(args, "--with-secret")
			} else {
				args = append(args, "--secret", "caller-supplied-secret")
			}
			output := runCommand(t, s, args...)
			if strings.Contains(output, "caller-supplied-secret") {
				t.Fatal("caller secret was printed")
			}
			if format == "json" {
				var result struct {
					Down       bool   `json:"down"`
					BypassPath string `json:"bypass_path"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil || !result.Down {
					t.Fatalf("invalid down JSON: %q (%v)", output, err)
				}
				if generated != strings.HasPrefix(result.BypassPath, "/") || (generated && len(result.BypassPath) < 17) {
					t.Fatalf("unexpected bypass path: %q", output)
				}
				if !generated && output != "{\"down\":true}\n" {
					t.Fatalf("caller secret added an output field: %q", output)
				}
			} else if generated {
				prefix, suffix := "Maintenance mode enabled.\nBypass: visit /", " to receive the bypass cookie.\n"
				if !strings.HasPrefix(output, prefix) || !strings.HasSuffix(output, suffix) {
					t.Fatalf("text output changed: %q", output)
				}
			} else if output != "Maintenance mode enabled.\n" {
				t.Fatalf("text output changed: %q", output)
			}
		}
	}
}

func TestMaintenanceCommandRejectsInvalidFormat(t *testing.T) {
	declarations, err := application.MaintenanceCommands()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"down", "up", "maintenance:status"} {
		for _, format := range []string{"yaml", "", "JSON"} {
			if _, err := registry.Parse([]string{action, "--format", format}, io.Discard); err == nil {
				t.Fatalf("%s accepted format %q", action, format)
			}
		}
	}
}

type statusStore struct {
	state  maintenance.State
	exists bool
	err    error
	loads  int
}

func (s *statusStore) Load(context.Context) (maintenance.State, bool, error) {
	s.loads++
	return s.state, s.exists, s.err
}

func (*statusStore) Save(context.Context, maintenance.State) error {
	return errors.New("status must not save maintenance state")
}

func TestMaintenanceStatusReadsSharedStore(t *testing.T) {
	since := time.Date(2026, 11, 11, 20, 30, 40, 123, time.FixedZone("MYT", 8*60*60))
	for _, tc := range []struct {
		name       string
		state      maintenance.State
		exists     bool
		json, text string
	}{
		{"absent", maintenance.State{Down: true, Since: since}, false, "{\"down\":false}\n", "Application is up\n"},
		{"up", maintenance.State{Since: since}, true, "{\"down\":false}\n", "Application is up\n"},
		{"down", maintenance.State{Down: true, Since: since, Message: "private", Secret: maintenance.SecretDigest{1}}, true, "{\"down\":true,\"since\":\"2026-11-11T12:30:40Z\"}\n", "Application is down since 2026-11-11T12:30:40Z\n"},
		{"zero since", maintenance.State{Down: true}, true, "{\"down\":true}\n", "Application is down since 0001-01-01T00:00:00Z\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &statusStore{state: tc.state, exists: tc.exists}
			app, err := foundation.NewBuilder().Register(foundation.Module{Name: "status.store", OnRegister: func(r *foundation.Registrar) error {
				return foundation.Provide[maintenance.Store](r, application.MaintenanceStoreKey, store)
			}}).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer app.Shutdown(t.Context())
			declarations, err := application.MaintenanceCommands()
			if err != nil {
				t.Fatal(err)
			}
			registry, err := cli.New(declarations...)
			if err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"text", "json"} {
				var output strings.Builder
				invocation, err := registry.Parse([]string{"maintenance:status", "--format", format}, &output)
				if err != nil {
					t.Fatal(err)
				}
				err = invocation.Run(t.Context(), app.Services(), cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output})
				want := tc.text
				if format == "json" {
					want = tc.json
				}
				if cli.Status(err) != cli.Success || output.String() != want {
					t.Fatalf("status %s: output=%q want=%q err=%v", format, output.String(), want, err)
				}
			}
			if store.loads != 2 {
				t.Fatalf("loaded %d times", store.loads)
			}
		})
	}
}

func TestMaintenanceStatusWhileLocalGateIsPaused(t *testing.T) {
	s := sharedMaintenance(t, t.TempDir())
	s.Features.Observability.Maintenance = true
	if output := runCommand(t, s, "maintenance:status"); output != "Application is up\n" {
		t.Fatal("status used local gate instead of absent shared record", output)
	}
	runCommand(t, s, "down")
	if output := runCommand(t, s, "maintenance:status", "--format", "json"); !strings.HasPrefix(output, `{"down":true,"since":"`) {
		t.Fatal("status blocked while down", output)
	}
}

func TestMaintenanceStatusFailuresAndDeclarationOrder(t *testing.T) {
	declarations, err := application.MaintenanceCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 3 {
		t.Fatalf("got %d declarations", len(declarations))
	}
	for i, name := range []cli.Name{"down", "up", "maintenance:status"} {
		registry, err := cli.New(declarations[i])
		if err != nil {
			t.Fatal(err)
		}
		description := registry.Describe()[0]
		if description.Name != name || !description.Maintenance {
			t.Fatal(description)
		}
	}
	cause := errors.New("store unavailable")
	store := &statusStore{err: cause}
	for _, missing := range []bool{false, true} {
		app, err := foundation.NewBuilder().Register(foundation.Module{Name: "status.store", OnRegister: func(r *foundation.Registrar) error {
			gate, err := maintenance.New(maintenance.Policy{})
			if err != nil {
				return err
			}
			if err := foundation.Provide(r, application.MaintenanceKey, gate); err != nil {
				return err
			}
			if missing {
				return nil
			}
			return foundation.Provide[maintenance.Store](r, application.MaintenanceStoreKey, store)
		}}).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer app.Shutdown(t.Context())
		registry, err := cli.New(declarations...)
		if err != nil {
			t.Fatal(err)
		}
		actions := []string{"maintenance:status"}
		if missing {
			actions = append(actions, "down", "up")
		}
		for _, action := range actions {
			var output strings.Builder
			invocation, err := registry.Parse([]string{action}, &output)
			if err != nil {
				t.Fatal(err)
			}
			err = invocation.Run(t.Context(), app.Services(), cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output})
			want := cause
			if missing {
				want = fault.Missing
			}
			if !errors.Is(err, want) || output.Len() != 0 {
				t.Fatalf("%s failure: %v, output=%q", action, err, output.String())
			}
			if missing && !strings.Contains(err.Error(), "maintenance commands require a configured maintenance store") {
				t.Fatal(err)
			}
		}
	}
}
