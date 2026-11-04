package idempotenthttp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestDetailedRejectionRollsBackAndReexecutes(t *testing.T) {
	var calls atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	var existingID model.ID[Order]
	app := startApp(t, pgtest.Isolate(t), Hooks{Calls: &calls, InTransaction: func(ctx context.Context, tx *database.Tx, _ BoundRequest) error {
		if !reject.Load() {
			return nil
		}
		var name string
		if err := database.ScanOne(ctx, tx, `SELECT name FROM idem_orders WHERE id=$1`, []any{existingID.String()}, &name); err != nil {
			return err
		}
		return OrderExists.New(OrderExistsDetails{ExistingOrderID: existingID, CurrentName: name})
	}}, func(settings *application.Settings) { settings.Features.Idempotency.Config.MaxRetainedPerCaller = 1 })
	db, err := app.app.Resources().Database()
	if err != nil {
		t.Fatal(err)
	}
	existing, err := orm.Insert(t.Context(), db, Orders.Table, orm.NumberSet(Orders.WorkspaceID, 1), orm.NumberSet(Orders.Caller, 1), orm.TextSet(Orders.Name, "first state"))
	if err != nil {
		t.Fatal(err)
	}
	existingID = existing.ID
	const key = "detailed-rejection-0001"
	const body = `{"name":"attempt"}`
	for i, name := range []string{"first state", "current state"} {
		if i > 0 {
			if _, err := orm.Update(t.Context(), db, Orders.Query(), Orders.Key(), existingID, orm.TextSet(Orders.Name, name)); err != nil {
				t.Fatal(err)
			}
		}
		response := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
		assertResponse(t, response, 409)
		var envelope struct {
			Code    string             `json:"error_code"`
			Details OrderExistsDetails `json:"details"`
		}
		if err := json.Unmarshal(response.body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Code != "order_exists" || envelope.Details.ExistingOrderID != existingID || envelope.Details.CurrentName != name || calls.Load() != int32(i+1) {
			t.Fatal("rejection did not read current state", string(response.body), calls.Load())
		}
		if scalar(t, db, `SELECT count(*) FROM idem_orders WHERE name='attempt'`) != 0 || scalar(t, db, `SELECT count(*) FROM foundry_idempotency`) != 0 || scalar(t, db, `SELECT count(*) FROM foundry_outbox`) != 0 {
			t.Fatal("rejection retained a business write, claim or outbox entry")
		}
	}
	// A one-outcome quota still admits success after both rejections. The same
	// key and body execute again; only this successful result becomes replayable.
	reject.Store(false)
	response := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, response, 201)
	if calls.Load() != 3 || scalar(t, db, `SELECT count(*) FROM idem_orders WHERE name='attempt'`) != 1 || scalar(t, db, `SELECT count(*) FROM foundry_idempotency WHERE completed_at IS NOT NULL`) != 1 {
		t.Fatal("rejections consumed quota or healthy request failed to commit")
	}
	replay := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, replay, 201)
	if string(response.body) != string(replay.body) || calls.Load() != 3 {
		t.Fatal("successful replay changed")
	}
}
