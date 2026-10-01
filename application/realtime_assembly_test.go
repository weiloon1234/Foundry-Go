package application_test

import (
	"context"
	"crypto/rand"
	"errors"
	stdhttp "net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/logging"
	redistest "github.com/weiloon1234/Foundry-Go/testkit/redis"
	client "github.com/weiloon1234/Foundry-Go/testkit/websocket"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type SharedNote struct {
	Text string `json:"text"`
}

func sharedNoteJSON() contract.JSON[SharedNote] {
	typ := reflect.TypeFor[SharedNote]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[SharedNote](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "text", Type: "text", Required: true}}}, {ID: "text", Kind: contract.StringKind}}})
}

// realtimeSettings configures the default in-process realtime connection.
func realtimeSettings() application.Settings {
	s := settings()
	s.Services.Realtime.Connections = infrastructure.RealtimeConnections{"default": infrastructure.DefaultRealtimeConnectionSettings()}
	return s
}

func TestSharedRealtimeServesUpgradesOnTheHTTPListener(t *testing.T) {
	s := realtimeSettings()
	s.Realtime.Enabled = true
	s.Realtime.Shared = true
	channel := websocket.Public[struct{}]("shared", websocket.DefineRooms(http.StringPath[string]()))
	updated := websocket.DefineOutgoing(channel, "updated", sharedNoteJSON())
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel, updated.Registration())}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.HTTP) }()
	address, err := app.HTTPReady(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if realtime, err := app.RealtimeReady(t.Context()); err != nil || realtime != address {
		t.Fatal("shared realtime must report the HTTP listener", realtime, err)
	}
	dial, dialCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer dialCancel()
	peer, err := client.Dial(dial, "ws://"+address+s.Realtime.Path, stdhttp.Header{"Origin": []string{"http://" + address}}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := peer.Send(dial, websocket.Request{Version: websocket.ProtocolVersion, Action: websocket.Subscribe, ID: "join", Channel: channel.ID()}); err != nil {
		t.Fatal(err)
	}
	if reply, err := peer.Receive(dial); err != nil || reply.Type != websocket.Subscribed {
		t.Fatal("shared listener did not serve the subscription", reply, err)
	}
	publisher, err := app.Resources().RealtimePublisher()
	if err != nil {
		t.Fatal(err)
	}
	id, err := websocket.Broadcast(t.Context(), publisher, channel, updated, SharedNote{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if event, err := peer.Receive(dial); err != nil || event.MessageID != id {
		t.Fatal("managed publisher did not reach the shared hub", event, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("graceful stop must return nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shared realtime did not drain")
	}
}

func TestShutdownBudgetMustContainListenerDrainAndStopDelay(t *testing.T) {
	for name, mutate := range map[string]func(*application.Settings){
		"http-grace": func(s *application.Settings) { s.ShutdownTimeout = s.HTTP.Server.ShutdownTimeout },
		"stop-delay": func(s *application.Settings) { s.StopDelay = s.ShutdownTimeout - s.HTTP.Server.ShutdownTimeout },
		"realtime": func(s *application.Settings) {
			s.HTTP.Enabled = false
			s.Realtime.Enabled = true
			s.ShutdownTimeout = s.Realtime.HTTP.ShutdownTimeout + s.Realtime.Config.DrainTimeout
		},
		"negative-delay": func(s *application.Settings) { s.StopDelay = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			s := realtimeSettings()
			mutate(&s)
			builder := application.New(s, quiet())
			if s.Realtime.Enabled {
				channel := websocket.Public[struct{}]("budget", websocket.DefineRooms(http.StringPath[string]()))
				builder.Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
					return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}}, nil
				})
			}
			if _, err := builder.Build(t.Context()); !errors.Is(err, fault.Invalid) {
				t.Fatal("shutdown budget without room for cleanup accepted", err)
			}
		})
	}
	s := settings()
	s.StopDelay = time.Second
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if app.StopDelay() != time.Second {
		t.Fatal("stop delay was not applied")
	}
}

func TestStandalonePublisherRequiresAClusterConnection(t *testing.T) {
	s := realtimeSettings()
	s.HTTP.Enabled = false
	s.Realtime.Publisher = true
	channel := websocket.Public[struct{}]("publisher", websocket.DefineRooms(http.StringPath[string]()))
	_, err := application.New(s, quiet()).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}}, nil
	}).Build(t.Context())
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("a local connection cannot publish across processes", err)
	}
	s.HTTP.Enabled = true
	s.Realtime.Publisher = false
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if _, err := app.Resources().RealtimePublisher(); !errors.Is(err, fault.Missing) {
		t.Fatal("unconfigured publication must be missing", err)
	}
}

func TestConfiguredMetricsIncludeLoggingAndRealtimeCollectors(t *testing.T) {
	s := realtimeSettings()
	s.Features.Observability.Enabled = true
	s.Realtime.Enabled = true
	s.Realtime.Shared = true
	// The injected default logger is borrowed; an owned file channel reports stats.
	audit := logging.DefaultChannelSettings()
	audit.Sink.Driver, audit.Sink.Path = logging.File, filepath.Join(t.TempDir(), "audit.jsonl")
	s.Log.Channels["audit"] = audit
	channel := websocket.Public[struct{}]("metrics", websocket.DefineRooms(http.StringPath[string]()))
	app, err := application.New(s, quiet()).HTTP(plainRoutes).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := app.Observability().WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{`foundry_log_records_total{channel="audit"} 0`, `foundry_log_write_failures_total{channel="audit"} 0`, "foundry_realtime_connections 0", "foundry_realtime_streaming 1", "go_goroutines "} {
		if !strings.Contains(output.String(), metric) {
			t.Fatalf("metrics exposition is missing %q", metric)
		}
	}
}

// Shutdown keeps the managed publisher's ownership until it actually exits,
// before the Redis client it borrows is closed.
func TestManagedPublisherShutdownWaitsForItsOperations(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Realtime.Publisher = true
	s.Services.Redis.Connections = infrastructure.RedisConnections{"default": infrastructure.RedisSettingsFromConfig(redistest.Config(t))}
	connection := infrastructure.DefaultRealtimeConnectionSettings()
	connection.Driver, connection.Redis = infrastructure.RedisRealtime, "default"
	connection.Cluster.Namespace = keyspace.Namespace{Application: "publisher-" + rand.Text()[:8], Environment: "test"}
	s.Services.Realtime.Connections = infrastructure.RealtimeConnections{"default": connection}
	channel := websocket.Public[struct{}]("publisher", websocket.DefineRooms(http.StringPath[string]()))
	app, err := application.New(s, quiet()).Realtime(func(application.Services) (application.RealtimeDeclarations, error) {
		return application.RealtimeDeclarations{Channels: []websocket.Registration{websocket.Register(channel)}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		stop(t, app)
		t.Fatal(err)
	}
	source, err := app.Resources().RealtimePublisher()
	if err != nil {
		stop(t, app)
		t.Fatal(err)
	}
	publisher, ok := source.(*websocket.Publisher)
	if !ok {
		stop(t, app)
		t.Fatal("a publisher-only process did not resolve the managed publisher")
	}
	stop(t, app)
	select {
	case <-publisher.Done():
	default:
		t.Fatal("shutdown finished before the publisher exited")
	}
}
