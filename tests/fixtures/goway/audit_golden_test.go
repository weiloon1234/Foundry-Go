package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"
	o "foundry.test/goway/oldmodels/hooks"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestAuditOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
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
		if old {
			declaration, err = o.NewMemberObserver("audit").Declare(func() o.MemberHooks {
				write := func(ctx context.Context, tx *database.Tx, c o.MemberChanges) error {
					history, err := c.Audit(o.MemberAuditPolicy{Body: record.Exclude})
					if err != nil {
						return err
					}
					return store(ctx, tx, history.Entry())
				}
				return o.MemberHooks{Created: write, Updated: write, Deleted: write, Restored: write}
			})
		} else {
			declaration, err = orm.NewObserver[n.Member]("audit").Declare(func() orm.Hooks[n.Member] {
				write := func(ctx context.Context, tx *database.Tx, c orm.Changes[n.Member]) error {
					history, err := orm.Audit(n.Members.Table, c, orm.AuditPolicyOf(n.Members.Table, orm.AuditField(n.Members.Body, record.Exclude)))
					if err != nil {
						return err
					}
					return store(ctx, tx, history.Entry())
				}
				return orm.Hooks[n.Member]{Created: write, Updated: write, Deleted: write, Restored: write}
			})
		}
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
		if old {
			q := o.QueryHookMembers()
			capture.show(must(q.Create(ctx, db, o.MemberDraft{}.SetID(1).SetName(" stored ").SetBody([]byte{1, 2}))))
			capture.show(must(q.Update(ctx, db, 1, o.MemberDraft{}.SetName(" new ").ClearNote())))
			capture.show(must(q.Delete(ctx, db, 1)))
			capture.show(must(q.Restore(ctx, db, 1)))
			capture.show(must(q.ForceDelete(ctx, db, 1)))
		} else {
			q := n.Members
			capture.show(must(q.Insert(ctx, db, q.Key().Set(1), q.Name.Set(" stored "), q.Body.Set([]byte{1, 2}))))
			capture.show(must(q.Update(ctx, db, 1, q.Name.Set(" new "), q.Note.SetNull())))
			capture.show(must(q.Delete(ctx, db, 1)))
			capture.show(must(q.Restore(ctx, db, 1)))
			capture.show(must(q.ForceDelete(ctx, db, q.Soft, 1)))
		}
		if captured != 5 {
			t.Fatal("audit stage count", captured)
		}
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{trace}))
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "audit_golden/1", transcripts[0], transcripts[1]) {
		t.Fatalf("lifecycle parity differs\nOLD:\n%s\nNEW:\n%s", transcripts[0], transcripts[1])
	}
	if len(transcripts) == 0 {
		t.Fatal("missing evidence")
	}
}
