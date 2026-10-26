package keyqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
)

func keyClauseParity(t *testing.T, got orm.Statement) {
	t.Helper()
	if !oracleEqual(t, "consumer/keyqueries/clause_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if len(got.Arguments()) == 0 {
	}
}

func TestCorrelatedSelectedKeySQLParity(t *testing.T) {
	q := models.Users.Query().Where(models.Users.Age.Gt(18))
	a := rows.AsRecord[purchaseAlias](rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(0)))), "purchase")

	c := rows.Correlate(rows.From(q), a)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), models.Users.ID)
	child := rows.InnerScope(c, rows.Scope(a))
	total, buyer := expr.At(child, models.Orders.TotalCents), expr.At(child, models.Orders.BuyerID)

	c = c.Where(expr.Equal(buyer, parent))
	bucket := expr.When(expr.OrderRow(total).Gt(1), expr.Row(total).Value().Param(1)).Else(expr.Row(total).Value().Param(0))

	count := expr.Count[rows.Correlation[models.User, rows.Alias[purchaseAlias, models.Order]]]()
	rank := expr.RowNumber(expr.WindowFor(child).OrderBy(total.Asc()))

	for _, tc := range []struct {
		name    string
		current rows.CorrelatedScalar[models.User, rows.Alias[purchaseAlias, models.Order], int64]
	}{
		{"computed_row", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, expr.Row(bucket).Value()), expr.Group(bucket)), expr.Row(bucket).Value().Key()), bucket.Desc())},
		{"aggregate", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, count.Value()), expr.Group(bucket)), count.Value().Key()), count.Desc())},
		{"window", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.SelectCorrelatedScalar(c, rank), rank.Key()), rank.Desc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, tc.current.Offset(1).Limit(2).Exists()))
			if err != nil {
				t.Fatal(err)
			}

			keyClauseParity(t, got)
		})
	}
}

func TestValueSetSelectedKeySQLParity(t *testing.T) {
	q := models.Users.Query()
	scalar := rows.Values(rows.SelectScalar(rows.From(q.Where(models.Users.Age.Gt(18))), expr.Row(expr.Value(models.Users.Nickname)).Value()))
	set := rows.UnionAllValues(scalar, scalar)

	key := expr.Row(set.Value()).Value()
	rank := expr.RowNumber(expr.Window[rows.Set[value.Nullable[string]]]{}.OrderBy(set.Value().Asc()))

	for _, tc := range []struct {
		name    string
		current rows.ValueSetQuery[value.Nullable[string]]
	}{
		{"nullable", rows.DistinctOnValueSet(set, key.Key()).OrderBy(key.Asc())},
		{"window", rows.DistinctOnValueSet(set, rank.Key()).OrderBy(rank.Asc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), tc.current.Offset(1).Limit(2))))
			if err != nil {
				t.Fatal(err)
			}

			keyClauseParity(t, got)
		})
	}
}

func TestPostgresORMComputedKeyClauses(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		q := models.Users.Query()
		a := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		c := rows.Correlate(rows.From(q), a)
		parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), models.Users.ID)
		scope := rows.InnerScope(c, rows.Scope(a))
		total, buyer := expr.At(scope, models.Orders.TotalCents), expr.At(scope, models.Orders.BuyerID)
		bucket := expr.When(expr.OrderRow(total).Gt(1), expr.Row(total).Value().Param(1)).Else(expr.Row(total).Value().Param(0))
		selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(buyer, parent)), expr.Row(bucket).Value())
		selected = rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(selected, expr.Group(bucket)), expr.Row(bucket).Value().Key()), bucket.Desc()).Limit(1)
		outer := rows.OrderScalar(rows.SelectScalar(rows.From(q), rows.CorrelatedScalarQuery(selected)), expr.Value(models.Users.Age).Asc())
		if got, err := rows.ReadScalar(outer).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Null[int64]()}) {
			t.Fatal("correlated computed keys", got, err)
		}
		scalar := rows.Values(rows.SelectScalar(rows.From(q), expr.Row(expr.Value(models.Users.Age)).Value()))
		set := rows.UnionAllValues(scalar, scalar)
		selectedSet := rows.DistinctOnValueSet(set, expr.Row(set.Value()).Value().Key()).OrderBy(set.Value().Asc())
		if got, err := rows.ReadValues(selectedSet).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 21, 22}) {
			t.Fatal("combined selected keys", got, err)
		}
		return nil
	})
}
