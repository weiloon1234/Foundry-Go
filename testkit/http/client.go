// Package http provides test-owned HTTP servers and bounded clients. Requests
// traverse the supplied production handler, including its authentication and
// middleware. It does not inject identities or replace response contracts.
package http

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

// Client embeds the production bounded client. Prefer relative request paths;
// URL also supports a separate realtime client against this owned server.
type Client struct {
	*httpclient.Client
	URL string
}

// New registers cleanup before starting the server. The handler must propagate
// request cancellation and return from its work before server cleanup completes.
// Register application cleanup before this helper so clients close first.
func New(t testing.TB, handler stdhttp.Handler) *Client {
	t.Helper()
	if handler == nil {
		t.Fatal("HTTP test client requires a handler")
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.BaseContext = func(net.Listener) context.Context { return t.Context() }
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	server.Start()
	return Connect(t, server.URL)
}

// Connect owns a bounded client for an already-running test HTTP application.
// The caller owns its server; register application cleanup before connecting.
func Connect(t testing.TB, url string) *Client {
	t.Helper()
	config := httpclient.DefaultConfig("test.http")
	config.BaseURL = url
	config.Timeout = 5 * time.Second
	config.AttemptTimeout = config.Timeout
	config.Retry = httpclient.NoRetries()
	client, err := httpclient.New(config, nil)
	if err != nil {
		t.Fatalf("create HTTP test client: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			t.Errorf("close HTTP test client: %v", err)
		}
	})
	return &Client{Client: client, URL: url}
}

// JSON uses the same generated request contract as production outbound calls.
func JSON[T any](ctx context.Context, request httpclient.Request, descriptor contract.JSON[T], payload T) (httpclient.Request, error) {
	return httpclient.JSON(ctx, request, descriptor, payload)
}

// DecodeJSON validates the complete response against its generated DTO contract.
// HTTP success/status assertions remain explicit.
func DecodeJSON[T any](ctx context.Context, response httpclient.Response, descriptor contract.JSON[T]) (T, error) {
	return httpclient.DecodeJSON(ctx, response, descriptor)
}
func AssertStatus(t testing.TB, response httpclient.Response, want int) {
	t.Helper()
	if response.Status() != want {
		t.Errorf("HTTP status: got %d, want %d", response.Status(), want)
	}
}
