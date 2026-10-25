package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"
	o "foundry.test/goway/oldmodels/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestSourceWriteOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
		now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
		closed := 0
		capture := &goldenCapture{onClose: func() { closed++ }, reply: func(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
			if strings.Contains(sql, "SELECT COUNT(*), COALESCE(MAX(") {
				return []string{"count", "max"}, [][]driver.Value{{int64(1), int64(1)}}
			}
			columns := []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}
			values := []driver.Value{int64(1), "stored", "label", []byte{1}, now, now, nil}
			if strings.Contains(sql, `RETURNING "hook_members"."id"`) && strings.Contains(sql, `"foundry_write_matches"`) {
				columns = append(columns, "matches")
				values = append(values, int64(1))
			}
			return columns, [][]driver.Value{values}
		}}
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
		check(err)
		var trace []string
		ctx := coremodels.LifecycleContext(t.Context(), &trace)
		var observer lifecycle.Declaration
		if old {
			observer, err = o.NewMemberObserver("source").Declare(func() o.MemberHooks { return o.Hooks("observer") })
		} else {
			observer, err = orm.NewObserver[n.Member]("source").Declare(func() orm.Hooks[n.Member] { return n.Hooks("observer") })
		}
		check(err)
		observers, err := lifecycle.NewObservers(observer)
		check(err)
		check(db.BindObservers(observers))
		check(db.Start(ctx))
		t.Cleanup(func() { check(db.Close(context.Background())) })
		if old {
			f := o.MemberFields()
			q := o.QueryHookMembers()
			source := q.Where(f.ID.Eq(1)).OrderBy(f.ID.Asc()).Limit(2)
			insertion := o.InsertMemberFrom(source).SelectID(query.Value(f.ID)).SelectBody(query.Value(f.Body)).SelectCreatedAt(query.Value(f.CreatedAt)).Values(o.MemberDraft{}.SetName(" normalized ").SetNote(coremodels.LabelInput{Text: " label "}))
			_, err = insertion.Returning(ctx, db, 2)
			check(err)
			_, err = insertion.Exec(ctx, db)
			check(err)
			update := o.UpdateMemberFrom(q.Where(f.Name.Ne("excluded")), source).MatchID(query.Value(f.ID)).SelectBody(query.Value(f.Body)).Values(o.MemberDraft{}.SetName(" updated ").SetNote(coremodels.LabelInput{Text: " updated "}))
			_, err = update.Returning(ctx, db, 2)
			check(err)
			_, err = update.Exec(ctx, db)
			check(err)
			_, err = o.UpdateMemberFrom(q, source).MatchNullableID(query.Nullable(query.Value(f.ID))).Values(o.MemberDraft{}.SetName(" literal ")).Exec(ctx, db)
			check(err)
			_, err = o.DeleteMemberUsing(q.WithTrashed(), source).MatchID(query.Value(f.ID)).Returning(ctx, db, 2)
			check(err)
			_, err = o.ForceDeleteMemberUsing(q.WithTrashed(), source).MatchID(query.Value(f.ID)).Returning(ctx, db, 2)
			check(err)
		} else {
			f := n.Members
			q := f.Query()
			source := r.From(q.Where(f.Key().Eq(1)).OrderBy(f.Key().Asc()).Limit(2))
			insertion := r.InsertFrom(source, q, r.MapInsert(f.ID, expr.Value(f.Key())), r.MapInsert(f.Body, expr.Value(f.Body)), r.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt))).Values(f.Name.Set(" normalized "), f.Note.Set(coremodels.LabelInput{Text: " label "}))
			_, err = insertion.Returning(ctx, db, 2)
			check(err)
			_, err = insertion.Exec(ctx, db)
			check(err)
			update := r.UpdateFrom(q.Where(f.Name.Ne("excluded")), source).Match(r.MatchSource(f.Key(), expr.Value(f.Key()))).Select(r.MapUpdate(f.Body, expr.Value(f.Body))).Values(f.Name.Set(" updated "), f.Note.Set(coremodels.LabelInput{Text: " updated "}))
			_, err = update.Returning(ctx, db, 2)
			check(err)
			_, err = update.Exec(ctx, db)
			check(err)
			_, err = r.UpdateFrom(q, source).Match(r.MatchNullableSource(f.Key(), expr.NullableRow(expr.Value(f.Key())))).Values(f.Name.Set(" literal ")).Exec(ctx, db)
			check(err)
			_, err = r.DeleteUsing(q.WithTrashed(), source).Match(r.MatchSource(f.Key(), expr.Value(f.Key()))).Returning(ctx, db, 2)
			check(err)
			_, err = r.ForceDeleteUsing(q.WithTrashed(), f.Soft, source).Match(r.MatchSource(f.Key(), expr.Value(f.Key()))).Returning(ctx, db, 2)
			check(err)
		}
		if len(trace) != 0 || closed != 5 {
			t.Fatal("source write callbacks or row closure", trace, closed)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "source_write_golden/1", transcripts[0], transcripts[1]) {
		t.Fatalf("source write parity differs\n%s", goldenDiff(transcripts[0], transcripts[1]))
	}
}
