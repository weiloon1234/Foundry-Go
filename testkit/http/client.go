// Package http provides test-owned HTTP servers and bounded clients. Requests
// traverse the supplied production handler, including its authentication and
// middleware. It does not inject identities or replace response contracts.
package http

import (
	"context"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

// Client embeds the production bounded client. Prefer relative request paths;
// URL also supports a separate realtime client against this owned server.
type Client struct {
	*httpclient.Client
	URL     string
	options options
}

// DefaultTimeout bounds each test request, including its response body.
const DefaultTimeout = 5 * time.Second

// Option configures a test HTTP client.
type Option func(*options) error

type options struct {
	timeout time.Duration
	headers stdhttp.Header
}

// WithTimeout replaces DefaultTimeout for slow handlers, such as streaming or
// large fixtures. It must be positive and at most one hour.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) error {
		if timeout <= 0 || timeout > time.Hour {
			return fault.New(fault.Invalid, "HTTP test timeout must be positive and at most one hour")
		}
		o.timeout = timeout
		return nil
	}
}

// WithHeaders sends these headers on every request. Each named header replaces
// the values a derived client inherits (see With) instead of adding a second
// value, so re-authenticating never sends two credentials. Values are copied;
// the production client still bounds and validates them.
func WithHeaders(headers stdhttp.Header) Option {
	return func(o *options) error {
		for name, values := range headers {
			o.headers.Del(name)
			for _, value := range values {
				o.headers.Add(name, value)
			}
		}
		return nil
	}
}

// withCookie replaces the named pair in the client's single Cookie header and
// keeps every other inherited cookie, as a browser would after a new login.
func withCookie(cookie *stdhttp.Cookie) Option {
	return func(o *options) error {
		pair := cookie.String()
		if pair == "" {
			return fault.New(fault.Invalid, "invalid test cookie")
		}
		pairs := make([]string, 0, 1)
		for _, line := range o.headers.Values("Cookie") {
			existing, err := stdhttp.ParseCookie(line)
			if err != nil {
				return fault.New(fault.Invalid, "inherited Cookie header is invalid")
			}
			for _, other := range existing {
				if other.Name != cookie.Name {
					pairs = append(pairs, other.String())
				}
			}
		}
		o.headers.Set("Cookie", strings.Join(append(pairs, pair), "; "))
		return nil
	}
}

// New registers cleanup before starting the server. The handler must propagate
// request cancellation and return from its work before server cleanup completes.
// Register application cleanup before this helper so clients close first.
func New(t testing.TB, handler stdhttp.Handler, configure ...Option) *Client {
	t.Helper()
	if handler == nil {
		t.Fatal("HTTP test client requires a handler")
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.BaseContext = func(net.Listener) context.Context { return t.Context() }
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	server.Start()
	return Connect(t, server.URL, configure...)
}

// Connect owns a bounded client for an already-running test HTTP application.
// The caller owns its server; register application cleanup before connecting.
func Connect(t testing.TB, url string, configure ...Option) *Client {
	t.Helper()
	settings := options{timeout: DefaultTimeout, headers: make(stdhttp.Header)}
	for _, option := range configure {
		if option == nil {
			t.Fatal("HTTP test client option is nil")
		}
		if err := option(&settings); err != nil {
			t.Fatalf("configure HTTP test client: %v", err)
		}
	}
	return connect(t, url, settings)
}

func connect(t testing.TB, url string, settings options) *Client {
	t.Helper()
	config := httpclient.DefaultConfig("test.http")
	config.BaseURL = url
	config.Timeout = settings.timeout
	config.AttemptTimeout = config.Timeout
	config.Retry = httpclient.NoRetries()
	config.Headers = settings.headers.Clone()
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
	return &Client{Client: client, URL: url, options: options{timeout: settings.timeout, headers: settings.headers.Clone()}}
}

// With returns an independent client for the same server with additional
// options, such as credential headers. This client remains unchanged.
func (c *Client) With(t testing.TB, configure ...Option) *Client {
	t.Helper()
	settings := options{timeout: c.options.timeout, headers: c.options.headers.Clone()}
	if settings.headers == nil {
		settings.headers = make(stdhttp.Header)
	}
	for _, option := range configure {
		if option == nil {
			t.Fatal("HTTP test client option is nil")
		}
		if err := option(&settings); err != nil {
			t.Fatalf("configure HTTP test client: %v", err)
		}
	}
	return connect(t, c.URL, settings)
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
