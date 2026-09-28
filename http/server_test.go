package http_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func config() foundryhttp.ServerConfig {
	c := foundryhttp.DefaultServerConfig()
	c.Address = "127.0.0.1:0"
	c.ShutdownTimeout = 100 * time.Millisecond
	return c
}

func await[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP operation did not complete")
		var zero T
		return zero
	}
}

func start(t *testing.T, handler stdhttp.Handler, logger *slog.Logger) (*foundryhttp.Server, string, context.CancelFunc, <-chan error) {
	t.Helper()
	server, err := foundryhttp.Prepare(handler, config(), logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		await(t, server.Done())
	})
	address, err := server.Ready(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return server, "http://" + address, cancel, finished
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func client(t *testing.T) *stdhttp.Client {
	t.Helper()
	transport := &stdhttp.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return &stdhttp.Client{Transport: transport, Timeout: 2 * time.Second}
}

func TestServerServesAndDrains(t *testing.T) {
	server, origin, cancel, finished := start(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "hello "+r.URL.Path)
	}), quietLogger())
	response, err := client(t).Get(origin + "/member")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != stdhttp.StatusOK || string(body) != "hello /member" {
		t.Fatalf("response: %d %q %v", response.StatusCode, body, err)
	}
	if err := server.Run(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatalf("second run: %v", err)
	}
	cancel()
	if err := await(t, finished); !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("normal drain: %v", err)
	}
}

func TestHijackedHandlerSharesShutdownGrace(t *testing.T) {
	entered, cancelled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	server, origin, cancel, finished := start(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		conn, _, err := stdhttp.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("native hijack capability lost: %v", err)
			return
		}
		defer conn.Close()
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		<-finish
	}), quietLogger())
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(origin, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	await(t, entered)
	cancel()
	await(t, cancelled)
	select {
	case <-server.Done():
		t.Fatal("hijacked handler was released before its synchronous work ended")
	default:
	}
	release.Do(func() { close(finish) })
	if err := await(t, finished); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hijacked handler grace was not bounded: %v", err)
	}
}

func TestActiveRequestCanFinishDuringGrace(t *testing.T) {
	entered, finish := make(chan context.Context, 1), make(chan struct{})
	var release sync.Once
	server, origin, cancel, finished := start(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		entered <- r.Context()
		<-finish
		_, _ = io.WriteString(w, "finished")
	}), quietLogger())
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	result := make(chan error, 1)
	c := client(t)
	go func() {
		response, err := c.Get(origin)
		if err == nil {
			var body []byte
			body, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if string(body) != "finished" {
				err = errors.New("grace interrupted response")
			}
		}
		result <- err
	}()
	requestContext := await(t, entered)
	cancel()
	if err := requestContext.Err(); err != nil {
		t.Fatalf("grace inherited immediate kernel cancellation: %v", err)
	}
	select {
	case <-server.Done():
		t.Fatal("server released active handler")
	default:
	}
	release.Do(func() { close(finish) })
	if err := await(t, result); err != nil {
		t.Fatal(err)
	}
	if err := await(t, finished); errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cooperative handler exceeded grace: %v", err)
	}
}

func TestGraceExpiryCancelsButRetainsHandler(t *testing.T) {
	entered, cancelled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	server, origin, cancel, finished := start(t, stdhttp.HandlerFunc(func(_ stdhttp.ResponseWriter, r *stdhttp.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		<-finish // Deliberately demonstrate an uncooperative handler.
	}), quietLogger())
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	requestDone := make(chan struct{})
	c := client(t)
	go func() {
		defer close(requestDone)
		response, _ := c.Get(origin)
		if response != nil {
			_ = response.Body.Close()
		}
	}()
	await(t, entered)
	cancel()
	await(t, cancelled)
	select {
	case <-server.Done():
		t.Fatal("shutdown deadline released a running handler")
	default:
	}
	release.Do(func() { close(finish) })
	if err := await(t, finished); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing grace failure: %v", err)
	}
	await(t, requestDone)
}

func TestStartupFailuresAndReadiness(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := config()
	c.Address = listener.Addr().String()
	server, err := foundryhttp.Prepare(stdhttp.NotFoundHandler(), c, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Run(t.Context()); err == nil {
		t.Fatal("occupied listener bound successfully")
	}
	if address, readyErr := server.Ready(t.Context()); address != "" || readyErr != err {
		t.Fatalf("readiness did not retain startup failure: %q %v", address, readyErr)
	}
	await(t, server.Done())
	server, err = foundryhttp.Prepare(stdhttp.NotFoundHandler(), config(), quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = server.Ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness wait ignored cancellation: %v", err)
	}
	if err = server.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled startup: %v", err)
	}
	if address, err := server.Ready(t.Context()); address != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled readiness: %q %v", address, err)
	}
	await(t, server.Done())
}

func TestNativeHeaderBoundsBeforeHandler(t *testing.T) {
	var called atomic.Bool
	c := config()
	c.MaxHeaderBytes = 512
	c.ReadHeaderTimeout = 100 * time.Millisecond
	server, err := foundryhttp.Prepare(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		called.Store(true)
	}), c, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() { cancel(); await(t, server.Done()) })
	ready, stopReady := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	for _, large := range []bool{true, false} {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		request := "GET / HTTP/1.1\r\nHost: localhost\r\nX-Partial: "
		if large {
			request += strings.Repeat("x", 16<<10) + "\r\n\r\n"
		}
		_, err = io.WriteString(conn, request)
		if err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		response, readErr := stdhttp.ReadResponse(bufio.NewReader(conn), nil)
		if large {
			if readErr != nil || response.StatusCode != stdhttp.StatusRequestHeaderFieldsTooLarge {
				_ = conn.Close()
				t.Fatalf("oversized header response: %v %v", response, readErr)
			}
			_ = response.Body.Close()
		} else {
			var timeout net.Error
			if readErr == nil || (errors.As(readErr, &timeout) && timeout.Timeout()) {
				_ = conn.Close()
				t.Fatalf("server did not close incomplete headers before client deadline: %v", readErr)
			}
		}
		_ = conn.Close()
	}
	if called.Load() {
		t.Fatal("invalid headers reached the application handler")
	}
	cancel()
	await(t, finished)
}

type synchronizedLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *synchronizedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.Write(p)
}

func (l *synchronizedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

func TestRawFailureIsolation(t *testing.T) {
	for _, failure := range []string{"panic", "abort", "goexit"} {
		t.Run(failure, func(t *testing.T) {
			var log synchronizedLog
			logger := slog.New(slog.NewTextHandler(&log, nil))
			_, origin, cancel, finished := start(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				switch failure {
				case "panic":
					panic("private-credential-marker")
				case "abort":
					panic(stdhttp.ErrAbortHandler)
				case "goexit":
					runtime.Goexit()
				}
			}), logger)
			response, err := client(t).Get(origin)
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil {
				t.Fatal("raw failed request returned a successful response")
			}
			cancel()
			if err := await(t, finished); errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failed handler leaked ownership: %v", err)
			}
			if strings.Contains(log.String(), "private-credential-marker") {
				t.Fatal("panic payload leaked to logs")
			}
			if (failure == "panic") != strings.Contains(log.String(), "HTTP handler failed") {
				t.Fatalf("raw failure logging: %q", log.String())
			}
		})
	}
}
