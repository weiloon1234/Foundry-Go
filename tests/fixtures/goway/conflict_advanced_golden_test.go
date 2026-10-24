package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	n "foundry.test/goway/models"
	o "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type conflictLookup struct{}

func TestAdvancedConflictOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
		stored := [][]driver.Value{{int64(1), "stored", true, nil, "3.00"}}
		capture := &goldenCapture{scriptedColumns: []string{"code", "name", "enabled", "note", "amount"}, scriptedBatches: [][][]driver.Value{stored, stored, stored, stored}}
		db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
		if err != nil {
			t.Fatal(err)
		}
		check(db.Start(t.Context()))
		t.Cleanup(func() { check(db.Close(context.Background())) })
		if old {
			f := o.WriteRecordFields()
			q := o.QueryWriteRecords()
			records := q.ConflictRows()
			stored := o.WriteRecordFieldsAt(records.Stored())
			proposed := o.WriteRecordFieldsAt(records.Proposed())
			policy := query.OnConflictKeys(query.Add(f.Amount, f.Amount).Group()).TargetWhere(f.Enabled.Eq(true), f.Name.Ne("O'Reilly\\path")).DoUpdate(query.SetConflictValue(f.Amount, query.Add(stored.Amount, proposed.Amount)), query.SetConflictValue(f.Note, proposed.Note)).Where(f.Name.Ne("bound"))
			policy = policy.WhereRows(query.Greater(proposed.Amount, stored.Amount))
			_, err = q.Upsert(t.Context(), db, o.WriteRecordDraft{}.SetName("input"), policy)
			check(err)
			// Deriving must not mutate the reusable index or update conditions.
			_ = policy.TargetWhere(f.Name.Eq("unused")).WhereRows(proposed.Enabled.Eq(false))
			_, err = q.Upsert(t.Context(), db, o.WriteRecordDraft{}.SetName("input"), policy)
			check(err)
			lookup := query.As[conflictLookup](q.Limit(1), "lookup")
			lf := o.WriteRecordFieldsAt(lookup.Scope())
			scalar := query.SelectValue(lookup, query.Value(lf.Note))
			p := query.OnConflict(f.Name).DoUpdate(query.SetConflictValue(f.Note, query.ConflictScalarNullableRowQuery(records, scalar)))
			_, err = q.Upsert(t.Context(), db, o.WriteRecordDraft{}.SetName("input"), p)
			check(err)
			c := query.Correlate(records, lookup)
			outer := o.WriteRecordFieldsAt(query.OuterScope(c, records.Stored()))
			inner := o.WriteRecordFieldsAt(query.InnerScope(c, lookup.Scope()))
			selected := query.SelectCorrelatedValue(c, query.Value(inner.Note)).Where(query.Greater(inner.Amount, outer.Amount)).Limit(1)
			p = query.OnConflict(f.Name).DoUpdate(query.SetConflictValue(f.Note, query.CorrelatedScalarNullableRowQuery(selected))).WhereRows(selected.Exists())
			_, err = q.Upsert(t.Context(), db, o.WriteRecordDraft{}.SetName("input"), p)
			check(err)
		} else {
			f := n.WriteRecords
			records := r.ConflictRowsOf(f.Query())
			stored := expr.At(records.Stored(), f.Amount)
			proposed := expr.At(records.Proposed(), f.Amount)
			policy := r.OnConflictKeys(expr.Group(expr.Add(expr.Value(f.Amount), expr.Value(f.Amount)))).TargetWhere(f.Enabled.Eq(true), f.Name.Ne("O'Reilly\\path")).DoUpdate(r.SetConflictValue(f.Amount, expr.Add(stored, proposed)), r.SetConflictValue(f.Note, expr.At(records.Proposed(), f.Note))).Where(f.Name.Ne("bound"))
			policy = r.ConflictWhereRows(policy, expr.Greater(proposed, stored))
			_, err = orm.Upsert(t.Context(), db, f.Query(), policy, f.Name.Set("input"))
			check(err)
			_ = r.ConflictWhereRows(r.ConflictTargetWhere(policy, expr.Value(f.Name).Eq("unused")), expr.At(records.Proposed(), f.Enabled).Eq(false))
			_, err = orm.Upsert(t.Context(), db, f.Query(), policy, f.Name.Set("input"))
			check(err)
			lookup := r.As[conflictLookup](f.Query().Limit(1), "lookup")
			scalar := r.SelectScalar(lookup, expr.Select(expr.At(r.Scope(lookup), f.Note)))
			p := orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(r.SetConflictValue(f.Note, r.ConflictScalarNullableRowQuery(records, scalar)))
			_, err = orm.Upsert(t.Context(), db, f.Query(), p, f.Name.Set("input"))
			check(err)
			c := r.Correlate(records, lookup)
			outer := expr.At(r.OuterScope(c, records.Stored()), f.Amount)
			inner := expr.At(r.InnerScope(c, r.Scope(lookup)), f.Amount)
			selected := r.SelectCorrelatedScalar(c, expr.Select(expr.At(r.InnerScope(c, r.Scope(lookup)), f.Note))).Where(expr.Greater(inner, outer)).Limit(1)
			p = r.ConflictWhereRows(orm.OnConflict(orm.ConflictKey(f.Name)).DoUpdate(r.SetConflictValue(f.Note, r.CorrelatedScalarNullableRowQuery(selected))), selected.Exists())
			_, err = orm.Upsert(t.Context(), db, f.Query(), p, f.Name.Set("input"))
			check(err)
		}
		transcripts = append(transcripts, capture.output.String())
	}
	if transcripts[0] != transcripts[1] {
		t.Fatalf("advanced conflict parity\n%s", goldenDiff(transcripts[0], transcripts[1]))
	}
	if !strings.Contains(transcripts[1], `O''Reilly`) || !strings.Contains(transcripts[1], `"excluded"."amount"`) {
		t.Fatal("missing schema literal or proposed calculation", transcripts[1])
	}
}
