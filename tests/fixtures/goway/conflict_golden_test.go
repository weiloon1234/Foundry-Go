package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestConflictOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
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

		observer, err = orm.NewObserver[n.Member]("upsert").Declare(func() orm.Hooks[n.Member] { return n.Hooks("observer") })

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

		f := n.Members
		draft := []orm.Assignment[n.Member]{f.Key().Set(1), f.Name.Set(" proposed "), f.Note.Set(coremodels.LabelInput{Text: " input "}), f.Body.Set([]byte{1})}
		policy := orm.OnConflict(orm.ConflictKey(f.Key())).DoUpdate(orm.ConflictIncoming(f.Name), orm.ConflictSet(f.Note, coremodels.LabelInput{Text: " literal "}))
		got, e := orm.Upsert(ctx, db, f.Query(), policy, draft...)
		check(e)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{got.IsSet()}))
		got, e = orm.Upsert(ctx, db, f.Query(), orm.OnConflictConstraint[n.Member]("hook_members_pkey").DoNothing(), draft...)
		check(e)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{got.IsSet()}))
		second := append([]orm.Assignment[n.Member]{f.Key().Set(2)}, draft[1:]...)
		items, e := orm.UpsertMany(ctx, db, f.Query(), policy, [][]orm.Assignment[n.Member]{draft, second})
		check(e)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{len(items)}))
		got, e = orm.Upsert(ctx, db, f.Query(), policy.DoUpdate(orm.ConflictNull(f.Note)), draft...)
		check(e)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{got.IsSet()}))
		got, e = orm.Upsert(ctx, db, f.Query(), policy.Where(f.Name.Eq("condition")), draft...)
		check(e)
		fmt.Fprintln(&capture.output, oracleSnapshot([]any{got.IsSet()}))

		if len(trace) != 0 {
			t.Fatal("upsert dispatched callbacks", trace)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "conflict_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}
