package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func TestModuleDrainsActualConsumerBeforeBorrowedTransportProviderCloses(t *testing.T) {
	transportKey := foundation.NewKey[http.RoundTripper]("test.http.transport")
	clientKey := foundation.NewKey[*httpclient.Client]("test.http.client")
	var dependencyClosed atomic.Bool
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, ContentLength: 0, Body: http.NoBody}, nil
	})
	adapter := foundation.Module{Name: "test.http.transport", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, transportKey, func(foundation.Resolver) (http.RoundTripper, error) { return transport, nil })
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		return r.OnShutdown("transport", func(context.Context) error { dependencyClosed.Store(true); return nil })
	}}
	module := httpclient.Module("test.http.client", clientKey, testConfig(), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (http.RoundTripper, error) { return foundation.Resolve(r, transportKey) })
	app, err := foundry.New().Register(module, adapter).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), clientKey)
	if err != nil {
		t.Fatal(err)
	}
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	operation := make(chan error, 1)
	go func() {
		operation <- client.Stream(context.Background(), client.Get("held"), func(ctx context.Context, _ *httpclient.StreamResponse) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
			if dependencyClosed.Load() {
				t.Error("borrowed dependency closed before callback exit")
			}
			return ctx.Err()
		})
	}()
	await(t, entered)
	shutdown := make(chan error, 1)
	go func() { shutdown <- app.Shutdown(context.Background()) }()
	await(t, canceled)
	if dependencyClosed.Load() {
		t.Fatal("dependency teardown crossed active client operation")
	}
	once.Do(func() { close(release) })
	if err := awaitResult(t, operation); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := awaitResult(t, shutdown); err != nil {
		t.Fatal(err)
	}
	if !dependencyClosed.Load() {
		t.Fatal("provider was not closed after drain")
	}
}
