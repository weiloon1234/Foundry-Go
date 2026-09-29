package http_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Global Compression, ETags and BrowserSessions follow the matched route's
// budget: a WithTimeout route can log in after the kernel RequestTimeout, an
// ordinary response completed after the deadline is still delivered, and only a
// credential staged before the deadline is withheld once the request ended.
func TestBrowserSessionsFollowRouteBudgetThroughGlobalWrappers(t *testing.T) {
	s := browserPrepare(t)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), s.sessions.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultBrowserSessionConfig()
	config.Clock = s.clock
	config.Cookie = "foundry_session" // Plain HTTP loopback test server.
	config.Options.Secure = false
	web, err := foundryhttp.NewBrowserSessions(registry, s.sessions, config)
	if err != nil {
		t.Fatal(err)
	}
	proof := browserProof(t)
	pause := func() { time.Sleep(150 * time.Millisecond) } // Past the 50ms kernel deadline.
	router, err := foundryhttp.NewRouter(
		browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithTimeout(5*time.Second).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
			pause()
			_, err := web.Login(ctx, proof, session.IssueOptions{})
			return foundryhttp.NoContent{}, err
		}),
		browserEndpoint("/late", foundryhttp.POST, foundryhttp.Public).Handle(func(context.Context, authInput) (foundryhttp.NoContent, error) {
			pause()
			return foundryhttp.NoContent{}, nil
		}),
		browserEndpoint("/withheld", foundryhttp.POST, foundryhttp.Public).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
			_, err := web.Login(ctx, proof, session.IssueOptions{})
			pause()
			return foundryhttp.NoContent{}, err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()), foundryhttp.ETags(foundryhttp.DefaultETagConfig()), web.Middleware())
	if err != nil {
		t.Fatal(err)
	}
	server := serveKernel(t, handler, 50*time.Millisecond)
	for _, tc := range []struct {
		path   string
		status int
		cookie bool
	}{{"/login", 204, true}, {"/late", 204, false}, {"/withheld", 0, false}} {
		request, err := http.NewRequestWithContext(t.Context(), "POST", server+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		cookie := len(response.Cookies()) != 0
		if tc.status != 0 && response.StatusCode != tc.status || tc.status == 0 && response.StatusCode < 500 || cookie != tc.cookie {
			t.Fatalf("%s: %d cookie=%v", tc.path, response.StatusCode, cookie)
		}
	}
}

// serveKernel runs a real kernel on a loopback port and returns its base URL.
func serveKernel(t *testing.T, handler http.Handler, requestTimeout time.Duration) string {
	t.Helper()
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.RequestTimeout = requestTimeout
	server, err := foundryhttp.Prepare(handler, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("kernel did not stop")
		}
	})
	ready, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	return "http://" + address
}
