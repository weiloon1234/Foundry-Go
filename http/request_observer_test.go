package http

import (
	"bytes"
	"context"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/observability"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRequestCompletionCoversRoutingAndEarlyFailures(t *testing.T) {
	route := DefineRoute(RouteSpec{ID: "observed.ok", Method: POST, Access: Public}, StaticPath("/ok"))
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("ok"))
	}))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := logging.JSON(&logs, logging.Options{})
	config := DefaultServerConfig()
	config.MaxBodyBytes = 4
	config.AccessLog = true
	var events []RequestEvent
	owner := newHandlerLifetime()
	handler := owner.wrap(router, logger, config, RequestObserverFunc(func(_ context.Context, e RequestEvent) { events = append(events, e) }))
	for _, test := range []struct {
		method, path, body string
		status             int
		route              RouteID
	}{{"POST", "/ok?private=value", "", 201, "observed.ok"}, {"GET", "/ok", "", 405, ""}, {"GET", "/absent?secret=value", "", 404, ""}, {"POST", "/ok", "large", 413, ""}} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer never-log-this")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatal(response.Code, test.status)
		}
		event := events[len(events)-1]
		if event.Result.Status != test.status || event.Route != test.route || event.RequestID == "" || string(event.RequestID) != response.Header().Get(RequestIDHeader) || event.Duration < 0 {
			t.Fatalf("bad completion: %+v", event)
		}
	}
	if len(events) != 4 || events[0].Bytes != 2 || events[0].Result.Outcome != observability.Succeeded || events[3].Result.Outcome != observability.Rejected {
		t.Fatal("completion metadata mismatch")
	}
	text := logs.String()
	for _, forbidden := range []string{"never-log-this", "private=value", "secret=value"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("automatic access log retained request data")
		}
	}
}
func TestCompletionObserverFailureDoesNotChangeResponse(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultServerConfig()
	events := 0
	handler := newHandlerLifetime().wrap(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(204) }), logger, config,
		RequestObserverFunc(func(context.Context, RequestEvent) { panic("private payload") }), RequestObserverFunc(func(context.Context, RequestEvent) { runtime.Goexit() }), RequestObserverFunc(func(context.Context, RequestEvent) { events++ }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 204 || events != 1 {
		t.Fatal("observer changed delivery or stopped later observers")
	}
}
func TestRejectedCompletionRemainsOwnedUntilCallbackReturns(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	owner := newHandlerLifetime()
	config := DefaultServerConfig()
	config.MaxBodyBytes = 1
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := owner.wrap(stdhttp.NotFoundHandler(), logger, config, RequestObserverFunc(func(context.Context, RequestEvent) { close(entered); <-release }))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("too big")))
	}()
	<-entered
	owner.seal()
	select {
	case <-owner.done:
		t.Fatal("observer ownership released early")
	default:
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("observer did not return")
	}
	select {
	case <-owner.done:
	case <-time.After(time.Second):
		t.Fatal("request ownership did not drain")
	}
}

func TestAdmittedCompletionRetainsRequestCapacity(t *testing.T) {
	owner := newHandlerLifetime()
	config := DefaultServerConfig()
	config.MaxConcurrentRequests = 1
	entered, release := make(chan struct{}), make(chan struct{})
	handler := owner.wrap(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(204) }), slog.New(slog.NewTextHandler(io.Discard, nil)), config, RequestObserverFunc(func(_ context.Context, e RequestEvent) {
		if e.Result.Status == 204 {
			close(entered)
			<-release
		}
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	<-entered
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, httptest.NewRequest("GET", "/", nil))
	if rejected.Code != 503 {
		t.Error("completion released admission early", rejected.Code)
	}
	close(release)
	<-done
}
