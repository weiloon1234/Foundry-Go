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
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func keyClauseParity(t *testing.T, got orm.Statement, want query.Statement) {
	t.Helper()
	if !oracleEqual(t, "consumer/keyqueries/clause_parity_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) || len(got.Arguments()) == 0 {
		t.Fatalf("key clause SQL or bindings changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}

func TestCorrelatedSelectedKeySQLParity(t *testing.T) {
	q, oldQ := models.Users.Query().Where(models.Users.Age.Gt(18)), models.QueryUsers().Where(models.UserFields().Age.Gt(18))
	a := rows.AsRecord[purchaseAlias](rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(0)))), "purchase")
	oldA := query.As[purchaseAlias](query.CTE("eligible", models.QueryOrders().Where(models.OrderFields().TotalCents.Gt(0))), "purchase")
	c, oldC := rows.Correlate(rows.From(q), a), query.Correlate(oldQ, oldA)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), models.Users.ID)
	child := rows.InnerScope(c, rows.Scope(a))
	total, buyer := expr.At(child, models.Orders.TotalCents), expr.At(child, models.Orders.BuyerID)
	oldParent := models.UserFieldsAt(query.OuterScope(oldC, oldQ.Scope()))
	oldChild := models.OrderFieldsAt(query.InnerScope(oldC, oldA.Scope()))
	c, oldC = c.Where(expr.Equal(buyer, parent)), oldC.Where(oldChild.BuyerID.EqColumn(oldParent.ID))
	bucket := expr.When(expr.OrderRow(total).Gt(1), expr.Select(total).Param(1)).Else(expr.Select(total).Param(0))
	oldBucket := query.When(oldChild.TotalCents.Gt(1), query.Param(oldChild.TotalCents, 1)).Else(query.Param(oldChild.TotalCents, 0))
	count, oldCount := expr.Count[rows.Correlation[models.User, rows.Alias[purchaseAlias, models.Order]]](), query.Count[query.Correlation[models.User, query.Alias[purchaseAlias, models.Order]]]()
	rank := expr.RowNumber(expr.WindowFor(child).OrderBy(total.Asc()))
	oldRank := query.RowNumber(query.WindowFor(oldC).OrderBy(oldChild.TotalCents.Asc()))
	for _, tc := range []struct {
		name    string
		current rows.CorrelatedScalar[models.User, rows.Alias[purchaseAlias, models.Order], int64]
		legacy  query.CorrelatedValueQuery[models.User, query.Alias[purchaseAlias, models.Order], int64]
	}{
		{"computed_row", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, expr.Select(bucket)), expr.Group(bucket)), expr.Select(bucket).Key()), bucket.Desc()), query.SelectCorrelatedValue(oldC, oldBucket.Value()).GroupBy(oldBucket.Group()).DistinctOnValues(oldBucket.Value().Key()).OrderBy(oldBucket.Desc())},
		{"aggregate", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(rows.SelectCorrelatedScalar(c, count.Value()), expr.Group(bucket)), count.Value().Key()), count.Desc()), query.SelectCorrelatedValue(oldC, oldCount.Value()).GroupBy(oldBucket.Group()).DistinctOnValues(oldCount.Value().Key()).OrderBy(oldCount.Desc())},
		{"window", rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.SelectCorrelatedScalar(c, rank), rank.Key()), rank.Desc()), query.SelectCorrelatedValue(oldC, oldRank).DistinctOnValues(oldRank.Key()).OrderBy(oldRank.Desc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, tc.current.Offset(1).Limit(2).Exists()))
			if err != nil {
				t.Fatal(err)
			}
			want, err := oldQ.Where(tc.legacy.Offset(1).Limit(2).Exists()).Compile()
			if err != nil {
				t.Fatal(err)
			}
			keyClauseParity(t, got, want)
		})
	}
}

func TestValueSetSelectedKeySQLParity(t *testing.T) {
	q, oldQ := models.Users.Query(), models.QueryUsers()
	scalar := rows.Values(rows.SelectScalar(rows.From(q.Where(models.Users.Age.Gt(18))), expr.Select(expr.Value(models.Users.Nickname))))
	set := rows.UnionAllValues(scalar, scalar)
	oldScalar := query.SelectValue(oldQ.Where(models.UserFields().Age.Gt(18)), query.Value(models.UserFields().Nickname))
	oldSet := oldScalar.UnionAll(oldScalar)
	key, oldKey := expr.Select(set.Value()), oldSet.Value()
	rank := expr.RowNumber(expr.Window[rows.Set[value.Nullable[string]]]{}.OrderBy(set.Value().Asc()))
	oldRank := query.RowNumber(query.Window[query.Set[value.Nullable[string]]]{}.OrderBy(oldSet.Value().Asc()))
	for _, tc := range []struct {
		name    string
		current rows.ValueSetQuery[value.Nullable[string]]
		legacy  query.ValueQuery[query.Set[value.Nullable[string]], value.Nullable[string]]
	}{
		{"nullable", rows.DistinctOnValueSet(set, key.Key()).OrderBy(key.Asc()), oldSet.DistinctOnValues(oldKey.Key()).OrderBy(oldKey.Asc())},
		{"window", rows.DistinctOnValueSet(set, rank.Key()).OrderBy(rank.Asc()), oldSet.DistinctOnValues(oldRank.Key()).OrderBy(oldRank.Asc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), tc.current.Offset(1).Limit(2))))
			if err != nil {
				t.Fatal(err)
			}
			want, err := oldQ.Where(query.ExistsQuery(oldQ, tc.legacy.Offset(1).Limit(2))).Compile()
			if err != nil {
				t.Fatal(err)
			}
			keyClauseParity(t, got, want)
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
		bucket := expr.When(expr.OrderRow(total).Gt(1), expr.Select(total).Param(1)).Else(expr.Select(total).Param(0))
		selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(buyer, parent)), expr.Select(bucket))
		selected = rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(rows.GroupByCorrelatedScalar(selected, expr.Group(bucket)), expr.Select(bucket).Key()), bucket.Desc()).Limit(1)
		outer := rows.OrderScalar(rows.SelectScalar(rows.From(q), rows.CorrelatedScalarQuery(selected)), expr.Value(models.Users.Age).Asc())
		if got, err := rows.ReadScalar(outer).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Null[int64]()}) {
			t.Fatal("correlated computed keys", got, err)
		}
		scalar := rows.Values(rows.SelectScalar(rows.From(q), expr.Select(expr.Value(models.Users.Age))))
		set := rows.UnionAllValues(scalar, scalar)
		selectedSet := rows.DistinctOnValueSet(set, expr.Select(set.Value()).Key()).OrderBy(set.Value().Asc())
		if got, err := selectedSet.All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 21, 22}) {
			t.Fatal("combined selected keys", got, err)
		}
		return nil
	})
}
