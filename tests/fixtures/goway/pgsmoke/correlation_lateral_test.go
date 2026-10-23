package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestStage6b3CorrelationAndLateral(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE correlation_nodes (id bigint PRIMARY KEY,parent_id bigint,note text,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO correlation_nodes VALUES (1,NULL,'root',NULL),(2,1,'second',NULL),(3,1,'third',NULL),(4,2,'grandchild',NULL),(5,1,'deleted',now()),(6,NULL,'empty',NULL)`)
	must(t, err)
	table := orm.Define("correlation_nodes", func(b orm.Builder[joinNode], r *joinNode) joinTable {
		return joinTable{orm.TableOf(b, &r.ID), orm.NullableColumnOf(b, &r.ParentID), orm.NullableTextOf(b, &r.Note), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	schema := rows.Define(func(b rows.Builder[joinRow], r *joinRow) joinRowFields {
		return joinRowFields{b.Row(), rows.NullableNumberOf(b, &r.LeftID), rows.NullableNumberOf(b, &r.RightID), rows.NullableTextOf(b, &r.Note)}
	})
	parent := rows.As[joinLeft](table.Query().Where(table.ParentID.IsNull()), "parent")
	child := rows.As[joinRight](table.Query(), "child")
	c := rows.Correlate(parent, child)
	outer, inner := rows.OuterScope(c, rows.Scope(parent)), rows.InnerScope(c, rows.Scope(child))
	c = c.Where(expr.Equal(expr.At(inner, table.ParentID), expr.NullableRow(expr.At(outer, table.Key()))))
	record := rows.SelectCorrelatedRecord(c, inner).OrderBy(expr.At(inner, table.Key()).Desc()).Limit(1)
	latest := rows.AsLateral[joinThird](record, "latest")
	joined := rows.LeftJoinLateral(parent, latest)
	ls, rs := rows.LeftScope(joined, rows.Scope(parent)), rows.NullableRightScope(joined, rows.LateralScope(latest))
	q := rows.Select(joined, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(ls, table.Key())).Value()), rows.Map(schema.RightID, expr.AtNullable(rs, table.Key())), rows.Map(schema.Note, expr.AtNullableValue(rs, table.Note))).OrderBy(expr.At(ls, table.Key()).Asc())
	got, err := q.All(ctx, db)
	must(t, err)
	if len(got) != 2 || got[0].RightID != value.Of(int64(3)) || !got[1].RightID.IsNull() {
		t.Fatal(got)
	}
	on := rows.On(expr.At(rows.Scope(parent), table.Key()), expr.At(rows.LateralScope(latest), table.ParentID)).WhereRight(expr.At(rows.LateralScope(latest), table.Key()).Eq(999))
	rejected := rows.LeftJoinLateral(parent, latest, on)
	ns := rows.NullableRightScope(rejected, rows.LateralScope(latest))
	ps := rows.LeftScope(rejected, rows.Scope(parent))
	got, err = rows.Select(rejected, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(expr.At(ps, table.Key())).Value()), rows.Map(schema.RightID, expr.AtNullable(ns, table.Key())), rows.Map(schema.Note, expr.AtNullableValue(ns, table.Note))).All(ctx, db)
	must(t, err)
	if len(got) != 2 || !got[0].RightID.IsNull() || !got[1].RightID.IsNull() {
		t.Fatal("ON did not preserve parents", got)
	}
	count := rows.SelectCorrelatedScalar(c, expr.Count[rows.Correlation[rows.Alias[joinLeft, joinNode], rows.Alias[joinRight, joinNode]]]().Value())
	ids := rows.SelectCorrelatedScalar(c, expr.Select(expr.At(inner, table.ParentID)))
	parentID := expr.At(rows.Scope(parent), table.Key())
	countQuery := rows.Select(parent, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(parentID).Value()), rows.MapSelected(schema.RightID, rows.CorrelatedScalarQuery(count)), rows.Map(schema.Note, expr.At(rows.Scope(parent), table.Note))).OrderBy(parentID.Asc())
	got, err = countQuery.All(ctx, db)
	must(t, err)
	if len(got) != 2 || got[0].RightID != value.Of(int64(2)) || got[1].RightID != value.Of(int64(0)) {
		t.Fatal("correlated aggregate or soft-delete scope", got)
	}
	got, err = countQuery.Where(c.Exists(), rows.InNullableCorrelatedQuery(parentID, ids)).All(ctx, db)
	must(t, err)
	if len(got) != 1 || got[0].LeftID != value.Of(int64(1)) {
		t.Fatal(got)
	}
	many := rows.SelectCorrelatedScalar(c, expr.Select(expr.At(inner, table.Key())))
	_, err = rows.Select(parent, schema, rows.MapSelected(schema.LeftID, expr.NullableRow(parentID).Value()), rows.MapSelected(schema.RightID, rows.CorrelatedScalarQuery(many)), rows.Map(schema.Note, expr.At(rows.Scope(parent), table.Note))).All(ctx, db)
	var sqlstate interface{ SQLState() string }
	if !errors.As(err, &sqlstate) || sqlstate.SQLState() != "21000" {
		t.Fatal("scalar cardinality was hidden", err)
	}
}
