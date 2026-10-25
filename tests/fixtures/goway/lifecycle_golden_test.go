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
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestLifecycleOldAndNewAgree(t *testing.T) {
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
		var declaration lifecycle.Declaration
		if old {
			declaration, err = o.NewMemberObserver("audit").Declare(func() o.MemberHooks { return o.Hooks("observer") })
		} else {
			declaration, err = orm.NewObserver[n.Member]("audit").Declare(func() orm.Hooks[n.Member] { return n.Hooks("observer") })
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
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{trace}))
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "lifecycle_golden/1", transcripts[0], transcripts[1]) {
		t.Fatalf("lifecycle parity differs\nOLD:\n%s\nNEW:\n%s", transcripts[0], transcripts[1])
	}
	if len(transcripts) == 0 {
		t.Fatal("missing evidence")
	}
}
