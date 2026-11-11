package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	outboundcapture "github.com/weiloon1234/Foundry-Go/httpclient/capture"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func outboundOrigins(app *application.App) []string {
	var origins []string
	for _, target := range app.Migrations() {
		for _, definition := range target.Definitions {
			if definition.Key.Origin == outboundcapture.MigrationOrigin {
				origins = append(origins, string(target.Connection)+"/"+target.Schema)
				break
			}
		}
	}
	return origins
}

func directClient(name httpclient.Name, base string) (foundation.Module, foundation.Key[*httpclient.Client]) {
	config := httpclient.DefaultConfig(name)
	config.BaseURL = base
	key := foundation.NewKey[*httpclient.Client]("fixture.client." + string(name))
	return httpclient.Module(foundation.ProviderID("fixture.client."+string(name)), key, config, nil, nil), key
}

func TestOutboundCaptureSettingsAndWiring(t *testing.T) {
	defaults := application.DefaultSettings().Features.OutboundCapture
	if !defaults.Enabled || !reflect.DeepEqual(defaults.Config, outboundcapture.DefaultConfig()) || defaults.Database != "" || defaults.Schema != "" {
		t.Fatal("outbound capture defaults changed")
	}
	if _, err := application.OutboundCaptureCommand(); err != nil {
		t.Fatal(err)
	}
	none, err := application.New(settings(), quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal("an application without a database failed to build", err)
	}
	t.Cleanup(func() { stop(t, none) })
	if _, err := none.Resources().OutboundCaptures(); !errors.Is(err, fault.Missing) || len(outboundOrigins(none)) != 0 {
		t.Fatal("an application without a database registered capture", err)
	}
	for _, enabled := range []bool{true, false} {
		s := captureSettings()
		s.Features.OutboundCapture.Enabled = enabled
		app, err := application.New(s, quiet()).HTTP(plainRoutes).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(t, app) })
		if _, err := app.Resources().OutboundCaptures(); err != nil {
			t.Fatal("outbound capture store did not resolve", enabled, err)
		}
		if origins := outboundOrigins(app); len(origins) != 1 || origins[0] != "default/public" {
			t.Fatal("outbound capture migration target changed", origins)
		}
	}
}

func TestOutboundCaptureBuildFailuresAndWarnings(t *testing.T) {
	unknown := captureSettings()
	unknown.Features.OutboundCapture.Database = "absent"
	if _, err := application.New(unknown, quiet()).Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("an explicit unknown capture database passed Build", err)
	}
	invalid := captureSettings()
	invalid.Features.OutboundCapture.Config.Exclude = []outboundcapture.Exclusion{{Host: "https://api.test"}}
	if _, err := application.New(invalid, quiet()).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("an invalid exclusion passed Build", err)
	}
	module, _ := directClient("gateway", "https://gateway.test")
	if _, err := application.New(captureSettings(), quiet()).Register(module).FullOutboundCapture("absent").Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("full capture for a client the application never builds passed Build", err)
	}
	named := captureSettings()
	named.Services.HTTPClients.Clients = infrastructure.HTTPClients{"named": infrastructure.DefaultHTTPClientSettings("named")}
	named.Services.HTTPClients.Default = "named"
	named.Features.OutboundCapture.Config.Exclude = []outboundcapture.Exclusion{{Client: "conditional"}, {Client: "gateway", Host: "*.sentry.io"}}
	var logs bytes.Buffer
	app, err := application.New(named, application.WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))).Register(module).FullOutboundCapture("gateway", "named").Build(t.Context())
	if err != nil {
		t.Fatal("full capture for constructed clients failed Build", err)
	}
	t.Cleanup(func() { stop(t, app) })
	if !strings.Contains(logs.String(), "outbound capture exclusion names a client the application does not construct") || !strings.Contains(logs.String(), "client=conditional") || strings.Contains(logs.String(), "client=gateway") {
		t.Fatal("unclaimed client-only exclusion was not logged once", logs.String())
	}
}

type outboundServer struct {
	*httptest.Server
	release chan struct{}
	entered chan struct{}
}

func newOutboundServer(t *testing.T) *outboundServer {
	t.Helper()
	s := &outboundServer{release: make(chan struct{}), entered: make(chan struct{}, 1)}
	s.Server = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/hold" {
			s.entered <- struct{}{}
			<-s.release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func outboundPostgresSettings(t *testing.T, migrated bool) (application.Settings, *database.DB, string) {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	if migrated {
		registry, err := migrate.New(outboundcapture.Migrations()...)
		if err != nil {
			t.Fatal(err)
		}
		config := migrate.DefaultPostgresConfig()
		config.Schema, config.SearchPath = scope.Schema(), scope.Schema()
		runner, err := migrate.NewPostgres(db, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Up(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	s := settings()
	s.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(scope.Config())
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	return s, db, scope.Schema()
}

func clientRows(t *testing.T, db *database.DB, schema string) map[string]int64 {
	t.Helper()
	stream, err := db.Primary().Query(t.Context(), `SELECT client, count(*) FROM "`+schema+`".foundry_outbound_captures GROUP BY client`)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	rows := map[string]int64{}
	for stream.Next() {
		var client string
		var n int64
		if err := stream.Scan(&client, &n); err != nil {
			t.Fatal(err)
		}
		rows[client] = n
	}
	return rows
}

// Settings-named and directly registered clients are captured with no call
// site code; httpclient.New is not; two applications keep separate recorders;
// and shutdown closes the store after every client drained, so a call in
// flight when shutdown starts is still stored.
func TestPostgresOutboundCaptureCoversModuleClientsAndDrainsOnShutdown(t *testing.T) {
	server := newOutboundServer(t)
	build := func(s application.Settings) (*application.App, *httpclient.Client) {
		s.Services.HTTPClients.Clients = infrastructure.HTTPClients{"named": {Config: func() httpclient.Config {
			config := httpclient.DefaultConfig("named")
			config.BaseURL = server.URL
			return config
		}()}}
		s.Services.HTTPClients.Default = "named"
		module, key := directClient("direct", server.URL)
		app, err := application.New(s, quiet()).Register(module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		direct, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			t.Fatal(err)
		}
		return app, direct
	}
	first, firstDB, firstSchema := outboundPostgresSettings(t, true)
	second, secondDB, secondSchema := outboundPostgresSettings(t, true)
	app, direct := build(first)
	other, otherDirect := build(second)
	named, err := app.Resources().HTTPClients.Client("named")
	if err != nil {
		t.Fatal(err)
	}
	standalone, err := httpclient.New(func() httpclient.Config {
		c := httpclient.DefaultConfig("standalone")
		c.BaseURL = server.URL
		return c
	}(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = standalone.Close(context.Background()) })
	for _, client := range []*httpclient.Client{named, direct, standalone, otherDirect} {
		if _, err := client.Do(t.Context(), client.Get("ok")); err != nil {
			t.Fatal(err)
		}
	}
	// Closing a client cancels its in-flight calls and waits for them; the
	// store closes afterwards, so the canceled attempt is still written.
	var held sync.WaitGroup
	held.Go(func() { _, _ = direct.Do(context.Background(), direct.Get("hold")) })
	<-server.entered
	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown <- app.Shutdown(ctx)
	}()
	time.Sleep(100 * time.Millisecond)
	close(server.release)
	held.Wait()
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	stop(t, other)
	if rows := clientRows(t, firstDB, firstSchema); !reflect.DeepEqual(rows, map[string]int64{"named": 1, "direct": 2}) {
		t.Fatal("module clients were not each captured, or a standalone client was", rows)
	}
	var result string
	if err := database.ScanOne(t.Context(), firstDB.Primary(), `SELECT result FROM "`+firstSchema+`".foundry_outbound_captures WHERE path = '/hold'`, nil, &result); err != nil || result != string(outboundcapture.ResultCanceled) {
		t.Fatal("the call in flight at shutdown was not stored", result, err)
	}
	if rows := clientRows(t, secondDB, secondSchema); !reflect.DeepEqual(rows, map[string]int64{"direct": 1}) {
		t.Fatal("two applications shared a recorder", rows)
	}
}

// A CLI process writes the rows of the calls it made before exiting, and a
// process that made no call never queries the table.
func TestPostgresOutboundCaptureInCLIProcesses(t *testing.T) {
	server := newOutboundServer(t)
	run := func(s application.Settings, call bool) string {
		module, key := directClient("direct", server.URL)
		command := foundation.Module{Name: "fixture.cli", OnRegister: func(r *foundation.Registrar) error {
			return r.Kernel(foundation.CLI, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
				return foundation.KernelFunc(func(ctx context.Context) error {
					if !call {
						return nil
					}
					client, err := foundation.Resolve(runtime.Services(), key)
					if err != nil {
						return err
					}
					_, err = client.Do(ctx, client.Get("ok"))
					return err
				}), nil
			})
		}}
		var logs bytes.Buffer
		app, err := application.New(s, application.WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))).Register(module, command).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := app.Run(t.Context(), foundation.CLI); err != nil {
			t.Fatal(err)
		}
		return logs.String()
	}
	migrated, db, schema := outboundPostgresSettings(t, true)
	logs := run(migrated, true)
	if rows := clientRows(t, db, schema); rows["direct"] != 1 {
		t.Fatal("a CLI process did not flush its capture", rows, logs)
	}
	missing, _, _ := outboundPostgresSettings(t, false)
	if logs := run(missing, false); strings.Contains(logs, "outbound capture table") {
		t.Fatal("a CLI process that made no call queried the table", logs)
	}
	if logs := run(missing, true); !strings.Contains(logs, "outbound capture table is unavailable") {
		t.Fatal("a call into an unmigrated table did not log its loss reason", logs)
	}
}
