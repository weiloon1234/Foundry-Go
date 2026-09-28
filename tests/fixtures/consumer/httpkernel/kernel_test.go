package httpkernel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var serverKey = foundation.NewKey[*foundryhttp.Server]("consumer.http")
var serviceKey = foundation.NewKey[*profileService]("consumer.profile")

type profileService struct{ closed atomic.Bool }

func await[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP fixture lifecycle did not complete")
		var zero T
		return zero
	}
}

func TestHTTPKernelRetainsInjectedDependencies(t *testing.T) {
	service := &profileService{}
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.ShutdownTimeout = 25 * time.Millisecond
	var constructed atomic.Int32
	app, err := foundry.New(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), foundation.WithShutdownTimeout(50*time.Millisecond)).Register(
		foundation.Module{Name: "profile", OnRegister: func(r *foundation.Registrar) error {
			return foundation.Provide(r, serviceKey, service)
		}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
			return r.OnShutdown("profile", func(context.Context) error {
				service.closed.Store(true)
				return nil
			})
		}},
		foundryhttp.Module("http", serverKey, config, func(resolver foundation.Resolver) (stdhttp.Handler, error) {
			profile, err := foundation.Resolve(resolver, serviceKey)
			if err != nil {
				return nil, err
			}
			constructed.Add(1)
			return stdhttp.HandlerFunc(func(_ stdhttp.ResponseWriter, r *stdhttp.Request) {
				close(entered)
				<-r.Context().Done()
				close(cancelled)
				<-release
				if profile.closed.Load() {
					t.Error("dependency closed while handler was still using it")
				}
			}), nil
		}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if constructed.Load() != 1 {
		t.Fatal("handler was not constructed once during Build")
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		await(t, app.Done())
	})
	server, err := foundation.Resolve(app.Services(), serverKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Run(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatalf("managed server allowed standalone startup: %v", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- app.Run(t.Context(), foundation.HTTP) }()
	readyContext, cancelReady := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelReady()
	address, err := server.Ready(readyContext)
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 2 * time.Second}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, _ := client.Get("http://" + address)
		if response != nil {
			_ = response.Body.Close()
		}
	}()
	await(t, entered)
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancelShutdown()
	if err := app.Shutdown(shutdownContext); !errors.Is(err, fault.Timeout) {
		t.Fatalf("expected pending handler ownership: %v", err)
	}
	await(t, cancelled)
	if app.State() != foundation.Stopping || service.closed.Load() {
		t.Fatal("shutdown timeout released handler dependencies")
	}
	releaseOnce.Do(func() { close(release) })
	await(t, app.Done())
	if !service.closed.Load() {
		t.Fatal("dependency was not closed after handler exit")
	}
	await(t, finished)
	await(t, requestDone)
}

func TestOtherKernelDoesNotBindHTTPListener(t *testing.T) {
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	app, err := foundry.New().Register(
		foundryhttp.Module("http", serverKey, config, func(foundation.Resolver) (stdhttp.Handler, error) {
			return stdhttp.NotFoundHandler(), nil
		}),
		foundation.Module{Name: "worker", OnRegister: func(r *foundation.Registrar) error {
			return r.Kernel(foundation.Worker, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
				server, err := foundation.Resolve(runtime.Services(), serverKey)
				if err != nil {
					return nil, err
				}
				return foundation.KernelFunc(func(ctx context.Context) error {
					wait, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
					defer cancel()
					if _, err := server.Ready(wait); !errors.Is(err, context.DeadlineExceeded) {
						return errors.New("HTTP listener started for worker kernel")
					}
					return nil
				}), nil
			})
		}},
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(t.Context(), foundation.Worker); err != nil {
		t.Fatal(err)
	}
}
