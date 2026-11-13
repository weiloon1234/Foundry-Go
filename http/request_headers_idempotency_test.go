package http

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func requestHeaderReplayStore(t *testing.T) (*idempotency.Store, *database.DB) {
	t.Helper()
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	definitions := append(idempotency.Migrations(), migrate.Definition{Key: migrate.Key{Origin: "test.headers", ID: "001_effects"}, Version: "v1", SQL: []string{`CREATE TABLE effects(id bigserial PRIMARY KEY)`}})
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = scope.Schema()
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	settings := idempotency.DefaultConfig()
	settings.Schema = scope.Schema()
	store, err := idempotency.New(db, keyspace.Namespace{Application: "test", Environment: "headers"}, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return store, db
}

func TestRequestHeaderIdempotencyReplayPostgres(t *testing.T) {
	store, db := requestHeaderReplayStore(t)
	route := DefineRoute(RouteSpec{ID: "runtime.headers.replay", Method: POST, Access: Public}, StaticPath("/headers"))
	e := DefineEndpoint(route, runtimeHeaders(), EmptyBody(), EmptyResponse(204))
	e = PrepareEndpoint(e, func(_ context.Context, in runtimeHeaderInput) (runtimeHeaderParams, NoBody, error) {
		// Different raw inputs with the same prepared value replay together.
		in.Query.Revision /= 10
		return in.Query, in.Body, nil
	})
	adapted := Idempotent(e, store, idempotency.Definition{ID: "headers", Version: 1})
	info, err := InspectIdempotent(adapted)
	if err != nil || info.Idempotency.Fingerprint != "prepared-path-query-headers-body-v2" {
		t.Fatal("missing v2 metadata", err)
	}
	scope, err := idempotency.NewScope("test", "caller")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	router, err := NewRouter(adapted.Handle(func(context.Context, runtimeHeaderInput) (idempotency.Scope, error) { return scope, nil }, func(ctx context.Context, tx *database.Tx, in runtimeHeaderInput) (NoContent, error) {
		calls++
		if in.Query.Revision != 1 {
			t.Error("handler did not receive prepared header")
		}
		_, err := tx.Exec(ctx, `INSERT INTO effects DEFAULT VALUES`)
		return NoContent{}, err
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		revision string
		note     []string
		status   int
	}{
		{"10", nil, 204}, {"19", nil, 204}, {"20", nil, 409}, {"10", []string{""}, 409}, {"10", nil, 204},
	} {
		r := httptest.NewRequest("POST", "/headers?X-Revision=query", nil)
		r.Header.Set("Idempotency-Key", "header-replay-key-0001")
		r.Header.Set("X-Revision", tc.revision)
		if tc.note != nil {
			r.Header["X-Note"] = tc.note
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
		assertHeaderVary(t, w.Header(), "X-Revision", "X-Note", "X-Enabled")
	}
	var effects int
	if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM effects`, nil, &effects); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || effects != 1 {
		t.Fatalf("replay ran effects: calls=%d rows=%d", calls, effects)
	}
}

func TestRequestHeaderFreeIdempotencyV1CompatibilityPostgres(t *testing.T) {
	store, _ := requestHeaderReplayStore(t)
	route := DefineRoute(RouteSpec{ID: "runtime.headers.legacy", Method: POST, Access: Public}, StaticPath("/legacy"))
	e := DefineEndpoint(route, EmptyQuery(), EmptyBody(), EmptyResponse(204))
	definition := idempotency.Definition{ID: "legacy", Version: 1}
	adapted := Idempotent(e, store, definition)
	info, err := InspectIdempotent(adapted)
	if err != nil || info.Idempotency.Fingerprint != "prepared-path-query-body-v1" {
		t.Fatal("v1 metadata changed", err)
	}
	// Seed using the pre-header identity and exact bytes. Replaying through the
	// current endpoint proves both the fingerprint and stored contract agree.
	identity := wireDigest(struct {
		Path        []PathParameterInfo
		Query       []QueryParameterInfo
		Body        *PayloadInfo
		Preparation bool
	}{info.Path, info.Query, info.Body, info.Preparation})
	legacy := idempotency.DefineInput(identity, func(context.Context, Input[NoPath, NoQuery, NoBody], int) ([]byte, error) {
		return []byte(`{"path":"/legacy","query":"","body":null}`), nil
	})
	operation, err := idempotency.Define(store, definition, legacy, idempotentEndpointOutputEncoding(adapted))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := idempotency.NewScope("test", "caller")
	if err != nil {
		t.Fatal(err)
	}
	key, err := idempotency.ParseKey("legacy-header-free-0001")
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.Run(t.Context(), scope, key, Input[NoPath, NoQuery, NoBody]{}, func(context.Context, *database.Tx, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
		return NoContent{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(adapted.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (idempotency.Scope, error) { return scope, nil }, func(context.Context, *database.Tx, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
		t.Error("legacy replay ran handler")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/legacy", nil)
	r.Header.Set("Idempotency-Key", "legacy-header-free-0001")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Vary") != "" {
		t.Fatalf("legacy replay changed: %d %s", w.Code, w.Body.String())
	}
}
