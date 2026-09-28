package idempotenthttp

import (
	"bytes"
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"sync/atomic"
	"testing"
	"time"
)

func TestTwoApplicationsOneCommittedHTTPOutcome(t *testing.T) {
	scope := pgtest.Isolate(t)
	var calls atomic.Int32
	var deny atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	hooks := Hooks{Calls: &calls, Deny: &deny, InTransaction: func(ctx context.Context, _ *database.Tx, in BoundRequest) error {
		if in.Request.Body.Name == "concurrent" {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	first, second := startApp(t, scope, hooks, nil), startApp(t, scope, hooks, nil)
	path, key := "/workspaces/1/orders", "concurrent-http-0001"
	done := make(chan wireResponse, 1)
	go func() { done <- first.send(t.Context(), path, "alice", key, `{"name":" concurrent ","memo":null}`) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first HTTP transaction did not enter")
	}
	waiting := second.send(t.Context(), path, "alice", key, `{"memo":null,"name":"concurrent"}`)
	assertResponse(t, waiting, 409)
	if !bytes.Contains(waiting.body, []byte("idempotency_in_progress")) || waiting.header.Get("Retry-After") == "" {
		close(release)
		t.Fatal("missing retry outcome")
	}
	close(release)
	original := <-done
	assertResponse(t, original, 201)
	replay := second.send(t.Context(), path, "alice", key, `{ "memo":null, "name":"concurrent" }`)
	assertResponse(t, replay, 201)
	if !bytes.Equal(original.body, replay.body) || original.header.Get("Location") != replay.header.Get("Location") || calls.Load() != 1 {
		t.Fatal("replay changed representation or repeated callback")
	}
	db, _ := first.app.Resources().Database()
	if scalar(t, db, `SELECT count(*) FROM idem_orders`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_idempotency WHERE completed_at IS NOT NULL`) != 1 {
		t.Fatal("claim/business/outbox were not atomic")
	}
	assertResponse(t, second.send(t.Context(), path, "alice", key, `{"name":"concurrent"}`), 409)
	assertResponse(t, second.send(t.Context(), path, "alice", key, `{"name":"concurrent","memo":""}`), 409)
	deny.Store(true)
	assertResponse(t, second.send(t.Context(), path, "alice", key, `{"name":"concurrent","memo":null}`), 403)
	deny.Store(false)
	assertResponse(t, second.send(t.Context(), path, "invalid", key, `{"name":"concurrent","memo":null}`), 401)
	for _, key := range []string{"", "tiny", "has spaces in this key"} {
		assertResponse(t, first.send(t.Context(), path, "alice", key, `{"name":"invalid"}`), 400)
	}
	assertResponse(t, second.send(t.Context(), path, "bob", key, `{"name":"other caller"}`), 201)
	assertResponse(t, second.send(t.Context(), "/workspaces/2/orders", "tenant", key, `{"name":"other tenant"}`), 201)
	if calls.Load() != 3 {
		t.Fatal("trusted scope isolation changed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for scalar(t, db, `SELECT count(*) FROM idem_deliveries`) < 3 {
		if time.Now().After(deadline) {
			t.Fatal("managed outbox did not deliver")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Current resource state is loaded even when the submission has a stored result.
	if _, err := QueryIdemWorkspaces().Update(t.Context(), db, 1, WorkspaceDraft{}.SetEnabled(false)); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, second.send(t.Context(), path, "alice", key, `{"name":"concurrent","memo":null}`), 403)
}
func TestHTTPFailuresRollbackResponsePreparationAndClaims(t *testing.T) {
	scope := pgtest.Isolate(t)
	var calls atomic.Int32
	app := startApp(t, scope, Hooks{Calls: &calls}, nil)
	db, _ := app.app.Resources().Database()
	for _, failure := range []string{"business", "encode", "oversized"} {
		key := "failed-http-" + failure
		body := `{"name":"` + failure + `","failure":"` + failure + `"}`
		status := 500
		if failure == "business" {
			status = 422
		}
		assertResponse(t, app.send(t.Context(), "/workspaces/1/orders", "alice", key, body), status)
		if scalar(t, db, `SELECT count(*) FROM idem_orders WHERE name=$1`, failure) != 0 {
			t.Fatal("response failure committed business writes")
		}
		if scalar(t, db, `SELECT count(*) FROM foundry_idempotency`) != 0 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 0 {
			t.Fatal("response failure retained claim or outbox")
		}
	}
	// A new schema contract must not execute an already committed key again.
	key := "schema-mismatch-001"
	body := `{"name":"schema"}`
	assertResponse(t, app.send(t.Context(), "/workspaces/1/orders", "alice", key, body), 201)
	if _, err := db.Exec(t.Context(), `UPDATE foundry_idempotency SET result_schema='future-schema'`); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, app.send(t.Context(), "/workspaces/1/orders", "alice", key, body), 503)
	if scalar(t, db, `SELECT count(*) FROM idem_orders`) != 1 {
		t.Fatal("incompatible result reexecuted")
	}
}
