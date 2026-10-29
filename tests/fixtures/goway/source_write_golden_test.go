package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestSourceWriteOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
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

		observer, err = orm.NewObserver[n.Member]("source").Declare(func() orm.Hooks[n.Member] { return n.Hooks("observer") })

		check(err)
		observers, err := lifecycle.NewObservers(observer)
		check(err)
		check(db.BindObservers(observers))
		check(db.Start(ctx))
		t.Cleanup(func() { check(db.Close(context.Background())) })

		f := n.Members
		q := f.Query()
		source := r.From(q.Where(orm.KeyEq(f.Key(), 1)).OrderBy(orm.KeyAsc(f.Key())).Limit(2))
		insertion := r.InsertFrom(source, q, r.MapInsert(orm.KeyExpr(f.ID), expr.Value(orm.KeyExpr(f.Key()))), r.MapInsert(orm.ColumnExpr(f.Body), expr.Value(orm.ColumnExpr(f.Body))), r.MapInsert(orm.TimeExpr(f.CreatedAt), expr.Value(orm.TimeExpr(f.CreatedAt)))).Values(orm.InputTextSet(f.Name, " normalized "), orm.NullableInputTextSet(f.Note, coremodels.LabelInput{Text: " label "}))
		_, err = insertion.Returning(ctx, db, 2)
		check(err)
		_, err = insertion.Exec(ctx, db)
		check(err)
		update := r.UpdateFrom(q.Where(orm.InputTextNe(f.Name, "excluded")), source).Match(r.MatchSource(orm.KeyExpr(f.Key()), expr.Value(orm.KeyExpr(f.Key())))).Select(r.MapUpdate(orm.ColumnExpr(f.Body), expr.Value(orm.ColumnExpr(f.Body)))).Values(orm.InputTextSet(f.Name, " updated "), orm.NullableInputTextSet(f.Note, coremodels.LabelInput{Text: " updated "}))
		_, err = update.Returning(ctx, db, 2)
		check(err)
		_, err = update.Exec(ctx, db)
		check(err)
		_, err = r.UpdateFrom(q, source).Match(r.MatchNullableSource(orm.KeyExpr(f.Key()), expr.NullableRow(expr.Value(orm.KeyExpr(f.Key()))))).Values(orm.InputTextSet(f.Name, " literal ")).Exec(ctx, db)
		check(err)
		_, err = r.DeleteUsing(q.WithTrashed(), source).Match(r.MatchSource(orm.KeyExpr(f.Key()), expr.Value(orm.KeyExpr(f.Key())))).Returning(ctx, db, 2)
		check(err)
		_, err = r.ForceDeleteUsing(q.WithTrashed(), f.Soft, source).Match(r.MatchSource(orm.KeyExpr(f.Key()), expr.Value(orm.KeyExpr(f.Key())))).Returning(ctx, db, 2)
		check(err)

		if len(trace) != 0 || closed != 5 {
			t.Fatal("source write callbacks or row closure", trace, closed)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if !oracleEqual(t, "source_write_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}
