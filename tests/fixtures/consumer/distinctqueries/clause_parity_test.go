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
	selected := rows.SelectScalar(rows.From(q.Where(models.Users.Age.Gt(18))), count.Value())
	grouped := rows.HavingScalar(rows.GroupByScalar(selected, expr.Group(expr.Value(models.Users.ID))), count.Gt(0))

	for _, tc := range []struct {
		name    string
		current rows.Scalar[models.User, int64]
	}{
		{"distinct", rows.OrderScalar(rows.DistinctScalar(grouped), count.Desc())},
		{"selected_key", rows.OrderScalar(rows.DistinctOnScalarValues(grouped, count.Value().Key()), count.Desc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, expr.Equal(rows.ScalarRowQuery(rows.From(q), tc.current), expr.NullableRow(count.Param(1)))))
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
	selected := rows.OrderScalar(rows.DistinctOnScalar(rows.SelectScalar(rows.From(q.Where(o.TotalCents.Gt(0))), expr.Row(expr.Value(o.TotalCents)).Value()), expr.Group(expr.Value(o.BuyerID))), expr.Value(o.BuyerID).Asc(), expr.Value(o.TotalCents).Desc())
	got, err := orm.Compile(expr.Where(q, rows.InQuery(expr.Value(o.TotalCents), selected)))
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
	source := rows.DistinctScalar(rows.SelectScalar(rows.From(q), expr.Row(expr.Value(models.Users.Nickname)).Value()))
	combined := rows.DistinctValues(rows.UnionAllValues(rows.Values(source), rows.Values(source)))
	combined = combined.OrderBy(combined.Value().Asc()).Offset(1).Limit(2)
	got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), combined)))
	if err != nil {
		t.Fatal(err)
	}

	assertClauseParity(t, got)
}
