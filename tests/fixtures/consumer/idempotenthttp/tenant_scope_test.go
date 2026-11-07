package idempotenthttp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestWorkspaceTenantScopeBeforeExecutionAndReplay(t *testing.T) {
	scope := pgtest.Isolate(t)
	var calls atomic.Int32
	app := startApp(t, scope, Hooks{Calls: &calls}, nil)
	errorPayload := func(response wireResponse) http.ErrorResponse {
		t.Helper()
		var payload http.ErrorResponse
		if err := json.Unmarshal(response.body, &payload); err != nil {
			t.Fatal(err)
		}
		// Each HTTP request has its own correlation identity.
		payload.RequestID = ""
		return payload
	}
	const body = `{"name":"tenant-scoped"}`
	const key = "tenant-workspace-0001"
	foreign := app.send(t.Context(), "/workspaces/2/orders", "alice", key, body)
	absent := app.send(t.Context(), "/workspaces/999/orders", "alice", key, body)
	assertResponse(t, foreign, 404)
	assertResponse(t, absent, 404)
	if !reflect.DeepEqual(errorPayload(foreign), errorPayload(absent)) || calls.Load() != 0 {
		t.Fatal("foreign workspace differs from an absent workspace or reached the handler")
	}
	db, err := app.app.Resources().Database()
	if err != nil {
		t.Fatal(err)
	}
	if scalar(t, db, `SELECT count(*) FROM foundry_idempotency`) != 0 || scalar(t, db, `SELECT count(*) FROM idem_orders`) != 0 {
		t.Fatal("missing workspace retained a claim or business write")
	}
	original := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, original, 201)
	replay := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, replay, 201)
	if !bytes.Equal(original.body, replay.body) || calls.Load() != 1 {
		t.Fatal("same-tenant replay changed or repeated the business callback")
	}
	if _, err := orm.Update(t.Context(), db, Workspaces.Query(), Workspaces.Key(), 1, orm.TextSet(Workspaces.Tenant, "tenant-b")); err != nil {
		t.Fatal(err)
	}
	foreignReplay := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, foreignReplay, 404)
	if !reflect.DeepEqual(errorPayload(absent), errorPayload(foreignReplay)) || calls.Load() != 1 {
		t.Fatal("replay bypassed current tenant scope")
	}
	if _, err := orm.Update(t.Context(), db, Workspaces.Query(), Workspaces.Key(), 1,
		orm.TextSet(Workspaces.Tenant, "tenant-a"), orm.ColumnSet(Workspaces.Enabled, false)); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, app.send(t.Context(), "/workspaces/1/orders", "alice", key, body), 403)
	if calls.Load() != 1 {
		t.Fatal("disabled workspace reexecuted a stored outcome")
	}
}
