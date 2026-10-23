package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Compiled/vetted in Lane B; the lead executes against PostgreSQL.
func TestLifecyclePersistedChangesRollbackAndCommit(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE hook_members(id bigint PRIMARY KEY,name text NOT NULL DEFAULT 'database',note text,body bytea NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE TABLE hook_events(member_id bigint NOT NULL,operation text NOT NULL)`)
	must(t, err)
	veto := errors.New("veto lifecycle write")
	commits := 0
	var captured orm.Changes[h.Member]
	var q h.MemberTable
	q = h.Declare(func() orm.Hooks[h.Member] {
		return orm.Hooks[h.Member]{
			Creating: func(ctx context.Context, tx *database.Tx, w *orm.Write[h.Member]) error {
				if !orm.WriteIsSet(w, q.Name.Set) {
					return orm.WriteSet(w, q.Name.Set(" supplied "))
				}
				return nil
			},
			Updated: func(ctx context.Context, tx *database.Tx, c orm.Changes[h.Member]) error {
				captured = c
				a, _ := c.After().Get()
				_, err := tx.Exec(ctx, `INSERT INTO hook_events(member_id,operation) VALUES($1,$2)`, a.ID, "update")
				if err != nil {
					return err
				}
				if a.Name == "reject" {
					return veto
				}
				return nil
			},
			AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[h.Member]) error { commits++; return nil },
		}
	})
	got, err := q.Insert(ctx, db, q.Key().Set(1), q.Body.Set([]byte{1, 2}))
	must(t, err)
	if got.Name != "supplied" || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() || commits != 1 {
		t.Fatal("hook/convention integration")
	}
	_, err = q.Update(ctx, db, 1, q.Name.Set(" reject "))
	if !errors.Is(err, veto) {
		t.Fatal(err)
	}
	persisted, err := q.Require(ctx, db, 1)
	must(t, err)
	if persisted.Name != "supplied" || commits != 1 {
		t.Fatal("aborted after hook persisted")
	}
	rows, err := db.Query(ctx, `SELECT count(*) FROM hook_events`)
	must(t, err)
	if !rows.Next() {
		t.Fatal("event count")
	}
	var count int64
	must(t, rows.Scan(&count))
	must(t, rows.Close())
	if count != 0 {
		t.Fatal("outbox-style transaction effect escaped rollback")
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := q.Update(ctx, tx, 1, q.Name.Set(" changed "))
		if err != nil {
			return err
		}
		if commits != 1 {
			return errors.New("callback ran before outer commit")
		}
		return nil
	}))
	before, _ := captured.Before().Get()
	after, _ := captured.After().Get()
	name, err := orm.ChangeOf(captured, q.Name)
	must(t, err)
	if before.Name != "supplied" || after.Name != "changed" || !name.Assigned() || !name.Changed() || commits != 2 {
		t.Fatal("locked original/returning/commit")
	}
	_, err = q.Query().UpdateAll(ctx, db, q.Name.Set("set"))
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("silent set bypass")
	}
	count, err = orm.WithoutModelHooks(q.Query()).UpdateAll(ctx, db, q.Name.Set("set"))
	must(t, err)
	if count != 1 || commits != 2 {
		t.Fatal("set acknowledgement")
	}
	_, err = q.Delete(ctx, db, 1)
	must(t, err)
	_, err = q.ForceDelete(ctx, db, 1)
	if !errors.Is(err, database.NotFound) {
		t.Fatal("force-delete visibility", err)
	}
	_, err = q.Restore(ctx, db, 1)
	must(t, err)
	_, err = q.Delete(ctx, db, 1)
	must(t, err)
	_, err = orm.ForceDelete(ctx, db, q.Query().WithTrashed(), q.Key(), int64(1))
	must(t, err)
}
