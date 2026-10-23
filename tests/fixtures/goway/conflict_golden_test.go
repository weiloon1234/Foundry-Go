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
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestConflictOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
		now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
		stored := [][]driver.Value{{int64(1), "stored", "label", []byte{1}, now, now, nil}}
		capture := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{stored, {}, stored, stored, {}}}
		var trace []string
		ctx := coremodels.LifecycleContext(t.Context(), &trace)
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
		if err != nil {
			t.Fatal(err)
		}
		var observer lifecycle.Declaration
		if old {
			observer, err = o.NewMemberObserver("upsert").Declare(func() o.MemberHooks { return o.Hooks("observer") })
		} else {
			observer, err = orm.NewObserver[n.Member]("upsert").Declare(func() orm.Hooks[n.Member] { return n.Hooks("observer") })
		}
		if err != nil {
			t.Fatal(err)
		}
		set, err := lifecycle.NewObservers(observer)
		if err != nil {
			t.Fatal(err)
		}
		check(db.BindObservers(set))
		check(db.Start(ctx))
		t.Cleanup(func() { check(db.Close(context.Background())) })
		if old {
			f := o.MemberFields()
			q := o.QueryHookMembers()
			draft := o.MemberDraft{}.SetID(1).SetName(" proposed ").SetNote(coremodels.LabelInput{Text: " input "}).SetBody([]byte{1})
			policy := query.OnConflict(f.ID).DoUpdate(f.Name.Incoming(), f.Note.Set(coremodels.LabelInput{Text: " literal "}))
			got, e := q.Upsert(ctx, db, draft, policy)
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			got, e = q.Upsert(ctx, db, draft, query.OnConflictConstraint[o.Member]("hook_members_pkey").DoNothing())
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			items, e := q.UpsertMany(ctx, db, []o.MemberDraft{draft, draft.SetID(2)}, policy)
			check(e)
			fmt.Fprintln(&capture.output, len(items))
			got, e = q.Upsert(ctx, db, draft, policy.DoUpdate(f.Note.SetNull()))
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			got, e = q.Upsert(ctx, db, draft, policy.Where(f.Name.Eq("condition")))
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
		} else {
			f := n.Members
			draft := []orm.Assignment[n.Member]{f.Key().Set(1), f.Name.Set(" proposed "), f.Note.Set(coremodels.LabelInput{Text: " input "}), f.Body.Set([]byte{1})}
			policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(orm.ConflictIncoming(f.Name), orm.ConflictSet(f.Note, coremodels.LabelInput{Text: " literal "}))
			got, e := orm.Upsert(ctx, db, f.Query(), policy, draft...)
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			got, e = orm.Upsert(ctx, db, f.Query(), orm.OnConflictConstraint[n.Member]("hook_members_pkey").DoNothing(), draft...)
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			second := append([]orm.Assignment[n.Member]{f.Key().Set(2)}, draft[1:]...)
			items, e := orm.UpsertMany(ctx, db, f.Query(), policy, [][]orm.Assignment[n.Member]{draft, second})
			check(e)
			fmt.Fprintln(&capture.output, len(items))
			got, e = orm.Upsert(ctx, db, f.Query(), policy.DoUpdate(orm.ConflictNull(f.Note)), draft...)
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
			got, e = orm.Upsert(ctx, db, f.Query(), policy.Where(f.Name.Eq("condition")), draft...)
			check(e)
			fmt.Fprintln(&capture.output, got.IsSet())
		}
		if len(trace) != 0 {
			t.Fatal("upsert dispatched callbacks", trace)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if transcripts[0] != transcripts[1] {
		t.Fatalf("conflict parity differs\n%s", goldenDiff(transcripts[0], transcripts[1]))
	}
}
