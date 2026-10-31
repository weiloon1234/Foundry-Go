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
	q := models.Users.Query().Where(orm.NumberGt(models.Users.Age, 18))
	a := rows.AsRecord[purchaseAlias](rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 0)))), "purchase")

	c := rows.Correlate(rows.From(q), a)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), orm.KeyExpr(models.Users.ID))
	child := rows.InnerScope(c, rows.Scope(a))
	total, buyer := expr.At(child, orm.NumberExpr(models.Orders.TotalCents)), expr.At(child, orm.ColumnExpr(models.Orders.BuyerID))

	c = c.Where(expr.Equal(buyer, parent))
	bucket := expr.When(expr.Gt(expr.OrderRow(total), 1), expr.ParamOf(expr.AsSelected(expr.Row(total)), 1)).Else(expr.ParamOf(expr.AsSelected(expr.Row(total)), 0))

	count := expr.Count[rows.Correlation[models.User, rows.Alias[purchaseAlias, models.Order]]]()
	rank := expr.RowNumber(expr.WindowFor(child).OrderBy(expr.Asc(total)))

	for _, tc := range []struct {
		name    string
		current rows.CorrelatedScalar[models.User, rows.Alias[purchaseAlias, models.Order], int64]
	}{
		{"computed_row", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, expr.AsSelected(expr.Row(bucket))), expr.Group(bucket)), expr.Key(expr.AsSelected(expr.Row(bucket)))), expr.Desc(bucket))},
		{"aggregate", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, expr.AsSelected(count)), expr.Group(bucket)), expr.Key(expr.AsSelected(count))), expr.DescValue(count))},
		{"window", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.SelectCorrelatedScalar(c, rank), expr.Key(rank)), expr.DescValue(rank))},
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
	scalar := rows.Values(rows.SelectScalar(rows.From(q.Where(orm.NumberGt(models.Users.Age, 18))), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))))))
	set := rows.UnionAllValues(scalar, scalar)

	key := expr.AsSelected(expr.Row(set.Value()))
	rank := expr.RowNumber(expr.Window[rows.Set[value.Nullable[string]]]{}.OrderBy(expr.Asc(set.Value())))

	for _, tc := range []struct {
		name    string
		current rows.ValueSetQuery[value.Nullable[string]]
	}{
		{"nullable", rows.DistinctOnValueSet(set, expr.Key(key)).OrderBy(expr.AscValue(key))},
		{"window", rows.DistinctOnValueSet(set, expr.Key(rank)).OrderBy(expr.AscValue(rank))},
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
		parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), orm.KeyExpr(models.Users.ID))
		scope := rows.InnerScope(c, rows.Scope(a))
		total, buyer := expr.At(scope, orm.NumberExpr(models.Orders.TotalCents)), expr.At(scope, orm.ColumnExpr(models.Orders.BuyerID))
		bucket := expr.When(expr.Gt(expr.OrderRow(total), 1), expr.ParamOf(expr.AsSelected(expr.Row(total)), 1)).Else(expr.ParamOf(expr.AsSelected(expr.Row(total)), 0))
		selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(buyer, parent)), expr.AsSelected(expr.Row(bucket)))
		selected = rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(selected, expr.Group(bucket)), expr.Key(expr.AsSelected(expr.Row(bucket)))), expr.Desc(bucket)).Limit(1)
		outer := rows.OrderScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(rows.CorrelatedScalarRowQuery(selected))), expr.Asc(expr.Value(orm.NumberExpr(models.Users.Age))))
		if got, err := rows.ReadScalar(outer).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Null[int64]()}) {
			t.Fatal("correlated computed keys", got, err)
		}
		scalar := rows.Values(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))))))
		set := rows.UnionAllValues(scalar, scalar)
		selectedSet := rows.DistinctOnValueSet(set, expr.Key(expr.AsSelected(expr.Row(set.Value())))).OrderBy(expr.Asc(set.Value()))
		if got, err := rows.ReadValues(selectedSet).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 21, 22}) {
			t.Fatal("combined selected keys", got, err)
		}
		return nil
	})
}
