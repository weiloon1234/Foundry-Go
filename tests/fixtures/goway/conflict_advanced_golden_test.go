package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	n "foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type conflictLookup struct{}

func TestAdvancedConflictOldAndNewAgree(t *testing.T) {
	var transcripts []string
	var statements []string
	{
		storedRows := [][]driver.Value{{int64(1), "stored", true, nil, "3.00"}}
		capture := &goldenCapture{scriptedColumns: []string{"code", "name", "enabled", "note", "amount"}, scriptedBatches: [][][]driver.Value{storedRows, storedRows, storedRows, storedRows}}
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
		if err != nil {
			t.Fatal(err)
		}
		check(db.Start(t.Context()))
		t.Cleanup(func() { check(db.Close(context.Background())) })

		// Deriving must not mutate the reusable index or update conditions.

		f := n.WriteRecords
		records := r.ConflictRowsOf(f.Query())
		stored := expr.At(records.Stored(), f.Amount)
		proposed := expr.At(records.Proposed(), f.Amount)
		policy := r.OnConflictKeys(expr.Group(expr.Add(expr.Value(f.Amount), expr.Value(f.Amount)))).TargetWhere(orm.Eq(f.Enabled, true), orm.Ne(f.Name, "O'Reilly\\path")).DoUpdate(r.SetConflictValue(f.Amount, expr.Add(stored, proposed)), r.SetConflictValue(f.Note, expr.At(records.Proposed(), f.Note))).Where(orm.Ne(f.Name, "bound"))
		policy = r.ConflictWhereRows(policy, expr.Greater(proposed, stored))
		_, err = orm.Upsert(t.Context(), db, f.Query(), policy, orm.Set(f.Name, "input"))
		check(err)
		_ = r.ConflictWhereRows(r.ConflictTargetWhere(policy, expr.Eq(expr.Value(f.Name), "unused")), expr.Eq(expr.At(records.Proposed(), f.Enabled), false))
		_, err = orm.Upsert(t.Context(), db, f.Query(), policy, orm.Set(f.Name, "input"))
		check(err)
		lookup := r.As[conflictLookup](f.Query().Limit(1), "lookup")
		scalar := r.SelectScalar(lookup, expr.AsSelected(expr.Row(expr.At(r.Scope(lookup), f.Note))))
		p := orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(r.SetConflictValue(f.Note, r.ConflictScalarNullableRowQuery(records, scalar)))
		_, err = orm.Upsert(t.Context(), db, f.Query(), p, orm.Set(f.Name, "input"))
		check(err)
		c := r.Correlate(records, lookup)
		outer := expr.At(r.OuterScope(c, records.Stored()), f.Amount)
		inner := expr.At(r.InnerScope(c, r.Scope(lookup)), f.Amount)
		selected := r.SelectCorrelatedScalar(c, expr.AsSelected(expr.Row(expr.At(r.InnerScope(c, r.Scope(lookup)), f.Note)))).Where(expr.Greater(inner, outer)).Limit(1)
		p = r.ConflictWhereRows(orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(r.SetConflictValue(f.Note, r.CorrelatedScalarNullableRowQuery(selected))), selected.Exists())
		_, err = orm.Upsert(t.Context(), db, f.Query(), p, orm.Set(f.Name, "input"))
		check(err)

		transcripts = append(transcripts, capture.output.String())
		statements = append(statements, capture.sql.String())
	}
	if !oracleEqual(t, "conflict_advanced_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}

	if !strings.Contains(statements[0], `O''Reilly`) || !strings.Contains(statements[0], `"excluded"."amount"`) {
		t.Fatal("missing schema literal or proposed calculation", transcripts[0])
	}
}
