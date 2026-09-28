package consumer_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type settings struct{ Name string }
type connection struct{ ready atomic.Bool }
type greeter struct {
	connection *connection
	name       string
}

func (g *greeter) Message(now time.Time) (string, error) {
	if !g.connection.ready.Load() {
		return "", errors.New("connection not ready")
	}
	return fmt.Sprintf("%s at %s", g.name, now.Format(time.RFC3339)), nil
}

var (
	connectionKey = foundation.NewKey[*connection]("fixture.connection")
	greeterKey    = foundation.NewKey[*greeter]("fixture.greeter")
	appName       = config.String("app.name", func(s *settings) *string { return &s.Name })
)

func TestConsumerBuildsBootsRunsAndCleansUp(t *testing.T) {
	schema, err := config.New(appName)
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := schema.Load(settings{Name: "default"}, config.Inputs[settings]{Overrides: []config.Override[settings]{appName.Set("Foundry")}})
	if err != nil {
		t.Fatal(err)
	}
	source := testkit.NewClock(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	var mu sync.Mutex
	var order []string
	record := func(item string) { mu.Lock(); defer mu.Unlock(); order = append(order, item) }
	conn := &connection{}
	messages := make(chan string, 1)
	infra := foundation.Module{Name: "fixture.infra", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, connectionKey, conn)
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		conn.ready.Store(true)
		record("boot infra")
		return r.OnShutdown("connection", func(context.Context) error { conn.ready.Store(false); record("close infra"); return nil })
	}}
	domain := foundation.Module{Name: "fixture.domain", Requires: []foundation.ProviderID{infra.Name}, OnRegister: func(r *foundation.Registrar) error {
		if err := foundation.Factory(r, greeterKey, func(resolver foundation.Resolver) (*greeter, error) {
			conn, err := foundation.Resolve(resolver, connectionKey)
			if err != nil {
				return nil, err
			}
			return &greeter{conn, settings.Name}, nil
		}); err != nil {
			return err
		}
		return r.Kernel(foundation.CLI, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
			g, err := foundation.Resolve(runtime.Services(), greeterKey)
			if err != nil {
				return nil, err
			}
			return foundation.KernelFunc(func(ctx context.Context) error {
				message, err := g.Message(runtime.Clock().Now())
				if err != nil {
					return err
				}
				messages <- message
				<-ctx.Done()
				record("stop kernel")
				return ctx.Err()
			}), nil
		})
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		record("boot domain")
		return r.OnShutdown("domain", func(context.Context) error {
			if !conn.ready.Load() {
				return errors.New("dependency closed early")
			}
			record("close domain")
			return nil
		})
	}}
	// Declare the dependent first: Foundry orders providers by dependencies.
	app := testkit.Start(t, foundry.New(foundation.WithClock(source)).Register(domain, infra))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx, foundation.CLI) }()
	select {
	case message := <-messages:
		if message != "Foundry at 2026-09-11T00:00:00Z" {
			t.Fatal(message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kernel did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kernel did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(order) != "[boot infra boot domain stop kernel close domain close infra]" || app.State() != foundation.Stopped {
		t.Fatalf("lifecycle: %v %s", order, app.State())
	}
}
