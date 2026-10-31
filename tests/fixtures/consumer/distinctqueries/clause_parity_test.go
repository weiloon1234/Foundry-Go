package distinctqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func assertClauseParity(t *testing.T, got orm.Statement) {
	t.Helper()
	if !oracleEqual(t, "consumer/distinctqueries/clause_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}

func TestScalarGroupingDistinctSQLParity(t *testing.T) {
	q := models.Users.Query()
	count := expr.Count[models.User]()
	selected := rows.SelectScalar(rows.From(q.Where(orm.NumberGt(models.Users.Age, 18))), expr.AsSelected(count))
	grouped := rows.HavingScalar(rows.GroupByScalar(selected, expr.Group(expr.Value(orm.KeyExpr(models.Users.ID)))), expr.GtValue(count, 0))

	for _, tc := range []struct {
		name    string
		current rows.Scalar[models.User, int64]
	}{
		{"distinct", rows.OrderScalar(rows.DistinctScalar(grouped), expr.DescValue(count))},
		{"selected_key", rows.OrderScalar(rows.DistinctOnScalarValues(grouped, expr.Key(expr.AsSelected(count))), expr.DescValue(count))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, expr.Equal(rows.ScalarRowQuery(rows.From(q), tc.current), expr.NullableRow(expr.ParamOf(count, 1)))))
			if err != nil {
				t.Fatal(err)
			}

			assertClauseParity(t, got)
		})
	}
}

func TestScalarDistinctOnUnselectedKeySQLParity(t *testing.T) {
	q := models.Orders.Query()
	o := models.Orders
	selected := rows.OrderScalar(rows.DistinctOnScalar(rows.SelectScalar(rows.From(q.Where(orm.NumberGt(o.TotalCents, 0))), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(o.TotalCents))))), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.Asc(expr.Value(orm.ColumnExpr(o.BuyerID))), expr.Desc(expr.Value(orm.NumberExpr(o.TotalCents))))
	got, err := orm.Compile(expr.Where(q, rows.InQuery(expr.Value(orm.NumberExpr(o.TotalCents)), selected)))
	if err != nil {
		t.Fatal(err)
	}

	assertClauseParity(t, got)
}

func TestRecordAndSetDistinctSQLParity(t *testing.T) {
	q := models.Users.Query()
	a := rows.As[personAlias](q, "person")
	record := rows.DistinctRecord(rows.SelectRecord(a, rows.Scope(a)))

	compare := func(t *testing.T, actual rows.RecordSource[models.User]) {
		t.Helper()
		alias := rows.AsRecord[resultAlias](rows.CTE("unique_people", actual), "chosen")

		got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), alias)))
		if err != nil {
			t.Fatal(err)
		}

		assertClauseParity(t, got)
	}
	t.Run("record", func(t *testing.T) { compare(t, record.Offset(1).Limit(2)) })
	t.Run("set", func(t *testing.T) {
		compare(t, rows.DistinctSet(rows.UnionAll(record, rows.ModelRecord(q))).Offset(1).Limit(2))
	})
}

func TestValueSetDistinctSQLParity(t *testing.T) {
	q := models.Users.Query()
	source := rows.DistinctScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))))))
	combined := rows.DistinctValues(rows.UnionAllValues(rows.Values(source), rows.Values(source)))
	combined = combined.OrderBy(expr.Asc(combined.Value())).Offset(1).Limit(2)
	got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), combined)))
	if err != nil {
		t.Fatal(err)
	}

	assertClauseParity(t, got)
}
