package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

func declarationStore(t *testing.T) *idempotency.Store {
	t.Helper()
	config := postgres.DefaultConfig()
	config.Host = "127.0.0.1"
	config.Database = "declaration"
	config.User = "declaration"
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Prepare(adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	store, err := idempotency.New(db, keyspace.Namespace{Application: "http", Environment: "declarations"}, idempotency.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	return store
}
func TestIdempotencyRejectsUnsupportedAssembly(t *testing.T) {
	store := declarationStore(t)
	definition := idempotency.Definition{ID: "empty", Version: 1}
	route := DefineRoute(RouteSpec{ID: "empty", Method: POST, Access: Public}, StaticPath("/empty"))
	empty := DefineEndpoint(route, EmptyQuery(), EmptyBody(), EmptyResponse(204))
	if err := empty.Idempotent(store, definition).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := DefineEndpoint(route, EmptyQuery(), EmptyBody(), DownloadResponse("text/plain")).Idempotent(store, definition).Validate(); err == nil {
		t.Fatal("download replay accepted")
	}
	if err := DefineEndpoint(route, EmptyQuery(), EmptyBody(), StreamResponse("text/plain")).Idempotent(store, definition).Validate(); err == nil {
		t.Fatal("stream replay accepted")
	}
	credential := empty
	credential.response.credentials = true
	if credential.Idempotent(store, definition).Validate() == nil {
		t.Fatal("credential issuance replay accepted")
	}
	safe := empty
	safe.route.spec.Method = GET
	if safe.Idempotent(store, definition).Validate() == nil {
		t.Fatal("safe-method idempotency accepted")
	}
	unknown := DefineMiddleware("custom", func(next stdhttp.Handler) (stdhttp.Handler, error) { return next, nil })
	if empty.WithMiddleware(unknown).Idempotent(store, definition).Validate() == nil {
		t.Fatal("unclassified response mutation accepted")
	}
	adapted := empty.WithMiddleware(unknown.PreservesIdempotentResponses()).Idempotent(store, definition)
	identity, _ := idempotency.NewScope("trusted", "caller")
	router, err := NewRouter(adapted.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (idempotency.Scope, error) { return identity, nil }, func(context.Context, *database.Tx, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
		t.Error("declaration-only handler ran")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMiddleware(router, unknown); err == nil {
		t.Fatal("outer unclassified middleware bypassed replay policy")
	}
	if _, err := ApplyMiddleware(router, unknown.PreservesIdempotentResponses()); err != nil {
		t.Fatal(err)
	}
	for _, headers := range [][]string{nil, {"tiny"}, {"valid-request-key-01", "another-valid-key-01"}} {
		request := httptest.NewRequest("POST", "/empty", nil)
		for _, key := range headers {
			request.Header.Add("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatal("invalid key reached database", response.Code)
		}
	}
	info, _ := adapted.Description()
	info.Idempotency.ApplicationHeaders = append(info.Idempotency.ApplicationHeaders, "Set-Cookie")
	next, _ := adapted.Description()
	if len(next.Idempotency.ApplicationHeaders) != 0 {
		t.Fatal("metadata mutation changed descriptor")
	}
}
func TestReplayHeaderAndSessionMutationBoundaries(t *testing.T) {
	for _, name := range []HeaderName{"Set-Cookie", "Authorization", "X-Request-ID", "Content-Length", "Content-Encoding", "Trailer"} {
		if _, err := replayHeaders([]ResponseHeader{{Name: name, Value: "private"}}); err == nil {
			t.Fatal("forbidden header accepted")
		}
	}
	if _, err := replayHeaders([]ResponseHeader{{Name: "Location", Value: "/one"}, {Name: "location", Value: "/two"}}); err == nil {
		t.Fatal("duplicate header accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := &browserSessionState{context: ctx, cancel: cancel, method: "POST"}
	defer state.close()
	ctx = context.WithValue(ctx, browserSessionKey{}, state)
	if err := bindBrowserResponse(ctx, false); err != nil {
		t.Fatal(err)
	}
	called := false
	err := state.operation(ctx, func(context.Context) (string, time.Time, error) {
		called = true
		return "private-cookie", time.Time{}, nil
	})
	if err == nil || called {
		t.Fatal("immutable idempotent response permitted session mutation")
	}
}
func TestIdempotencyRetryErrorsRemainDeclared(t *testing.T) {
	for _, code := range []idempotency.Code{idempotency.InProgress, idempotency.Capacity, idempotency.Unavailable} {
		err := idempotencyHTTPError(code, 150*time.Millisecond)
		r := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		if writeErr := WriteError(w, r, err); writeErr != nil {
			t.Fatal(writeErr)
		}
		if w.Header().Get("Retry-After") != "1" {
			t.Fatal("retry delay missing")
		}
		if !errors.Is(err, code) {
			t.Fatal("underlying outcome lost")
		}
	}
}
