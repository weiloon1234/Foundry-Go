package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestAuditOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
		now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
		row := func(name string, deleted driver.Value) [][]driver.Value {
			return [][]driver.Value{{int64(1), name, nil, []byte{1, 2}, now, now, deleted}}
		}
		capture := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{
			row("stored", nil),                      // create returning
			row("stored", nil), row("trigger", nil), // lock, update returning
			row("trigger", nil), row("trigger", now), // soft delete
			row("trigger", now), row("trigger", nil), // restore
			row("trigger", nil), row("trigger", nil), // physical delete
		}}
		var trace []string
		ctx := coremodels.LifecycleContext(t.Context(), &trace)
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
		if err != nil {
			t.Fatal(err)
		}
		captured := 0
		store := func(ctx context.Context, tx *database.Tx, entry record.Entry) error {
			payload, err := entry.Payload()
			if err != nil {
				return err
			}
			captured++
			_, err = tx.Exec(ctx, "INSERT INTO audit_probe(payload) VALUES($1)", payload)
			return err
		}
		var declaration lifecycle.Declaration

		declaration, err = orm.NewObserver[n.Member]("audit").Declare(func() orm.Hooks[n.Member] {
			write := func(ctx context.Context, tx *database.Tx, c orm.Changes[n.Member]) error {
				history, err := orm.Audit(n.Members.Table, c, orm.AuditPolicyOf(n.Members.Table, orm.AuditField(orm.ColumnExpr(n.Members.Body), record.Exclude)))
				if err != nil {
					return err
				}
				return store(ctx, tx, history.Entry())
			}
			return orm.Hooks[n.Member]{Created: write, Updated: write, Deleted: write, Restored: write}
		})

		if err != nil {
			t.Fatal(err)
		}
		observers, err := lifecycle.NewObservers(declaration)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.BindObservers(observers); err != nil {
			t.Fatal(err)
		}
		if err = db.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { check(db.Close(context.Background())) })

		q := n.Members
		capture.show(must(orm.Insert(ctx, db, q.Table, orm.KeySet(q.Key(), 1), orm.InputTextSet(q.Name, " stored "), orm.ColumnSet(q.Body, []byte{1, 2}))))
		capture.show(must(orm.Update(ctx, db, q.Table.Query(), q.Table.Key(), 1, orm.InputTextSet(q.Name, " new "), orm.NullableInputTextSetNull(q.Note))))
		capture.show(must(orm.Delete(ctx, db, q.Table.Query(), q.Table.Key(), 1)))
		capture.show(must(orm.Restore(ctx, db, q.Table, 1)))
		capture.show(must(orm.ForceDelete(ctx, db, q.Table.Query(), q.Soft, q.Table.Key(), 1)))

		if captured != 5 {
			t.Fatal("audit stage count", captured)
		}
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{trace}))
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "audit_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}

	if len(transcripts) == 0 {
		t.Fatal("missing evidence")
	}
}
