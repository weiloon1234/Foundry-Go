package keyqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresComputedGroupKeys(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		base := models.Users.Query()
		u := models.Users
		bucket := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 1)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
		if got, err := rowset.Read(buckets()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []keyqueries.BucketCount{{Bucket: 0, Count: 1}, {Bucket: 1, Count: 2}}) {
			t.Fatal("computed group hydration or parameter identity", got, err)
		}
		if got, err := rowset.Read(rowset.Having(buckets(), expr.Grouped(expr.Eq(bucket, 1)))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []keyqueries.BucketCount{{Bucket: 1, Count: 2}}) {
			t.Fatal("computed group HAVING", got, err)
		}
		// A larger selected key must reuse its grouped child's parameter IDs.
		nested := expr.NullIf(bucket, expr.Param(orm.NumberExpr(u.Age), 1))
		q := rowset.OrderScalar(rowset.DistinctOnScalarValues(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(nested)), expr.Group(bucket)), expr.Key(expr.AsSelected(nested))), expr.Asc(nested))
		if got, err := rowset.ReadScalar(q).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Of(0), value.Null[int]()}) {
			t.Fatal("grouped child inside selected DISTINCT key", got, err)
		}
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.DistinctScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(nested)), expr.Group(bucket))), expr.Asc(nested))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Of(0), value.Null[int]()}) {
			t.Fatal("ordinary DISTINCT with grouped parameters", got, err)
		}
		constant := expr.Param(orm.NumberExpr(u.Age), 1)
		if got, err := rowset.ReadScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Count[models.User]())), expr.Group(constant))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{3}) {
			t.Fatal("integer parameter treated as GROUP BY ordinal", got, err)
		}
		// NULL and an empty text value form different groups.
		nickname := expr.When(expr.ModelPredicate(orm.NumberEq(u.Age, 20)), expr.Param(orm.TextExpr(u.Email), "")).ElseNull()
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Count[models.User]())), expr.Group(nickname)), expr.Asc(nickname))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("nullable group lost empty value", got, err)
		}
		// An empty membership operand is compiled for validation then discarded.
		choice := expr.When(expr.In(constant), expr.Param(orm.NumberExpr(u.Age), 9)).Else(expr.Param(orm.NumberExpr(u.Age), 7))
		if got, err := rowset.ReadScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(choice)), expr.Group(constant))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{7}) {
			t.Fatal("empty membership retained discarded key bindings", got, err)
		}
		return nil
	})
}

func TestPostgresComputedPartitionsAndDistinct(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base := models.Users.Query()
		u := models.Users
		bucket := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 1)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
		partition := expr.WindowFor(rowset.ModelScope(base)).PartitionBy(expr.Group(bucket)).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(u.Age))))
		rank := expr.RowNumber(partition)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(base), rank), expr.Asc(expr.Value(orm.NumberExpr(u.Age))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1, 2}) {
			t.Fatal("computed row partition", got, err)
		}
		if got, err := rowset.Read(rowset.OrderBy(rowset.DistinctOn(rowset.FromModel(base), expr.Group(bucket)), expr.Asc(bucket), expr.Desc(expr.Value(orm.NumberExpr(u.Age))))).All(t.Context(), tx); err != nil || len(got) != 2 || got[0].ID != users[0].ID || got[1].ID != users[2].ID {
			t.Fatal("computed DISTINCT ON precedence", got, err)
		}
		// Window outputs are evaluated before DISTINCT ON.
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.DistinctOnScalarValues(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(u.Age))))), expr.Key(rank)), expr.AscValue(rank), expr.Asc(expr.Value(orm.NumberExpr(u.Age))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 22}) {
			t.Fatal("window DISTINCT key", got, err)
		}
		count := expr.Count[models.User]()
		groupedWindow := expr.WindowFor(rowset.ModelScope(base)).PartitionByValues(expr.Key(expr.AsSelected(count)))
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.RowNumber(groupedWindow)), expr.Group(bucket)), expr.AscValue(count))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("aggregate partition over grouped rows", got, err)
		}
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.DistinctOnScalarValues(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(count)), expr.Group(bucket)), expr.Key(expr.AsSelected(count))), expr.AscValue(count))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("aggregate DISTINCT key", got, err)
		}
		combined := rowset.UnionAllValues(rowset.Values(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(u.Age)))))), rowset.Values(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(u.Age)))))))
		if got, err := rowset.ReadValues(rowset.DistinctOnValueSet(combined, expr.Key(expr.AsSelected(combined.Value()))).OrderBy(expr.Asc(combined.Value()))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 21, 22}) {
			t.Fatal("combined value DISTINCT key", got, err)
		}
		return nil
	})
}

type eligibleAlias struct{}
type purchaseAlias struct{}
type groupedAlias struct{}

func TestPostgresComputedKeyComposition(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		base := models.Users.Query()
		u := models.Users
		eligible := rowset.AsRecord[eligibleAlias](rowset.CTE("eligible", rowset.ModelRecord(base.Where(orm.NumberGt(u.Age, 20)))), "eligible_users")
		f := rowset.Scope(eligible)
		key := expr.When(rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), rowset.SelectScalar(eligible, expr.AsSelected(expr.Row(expr.At(f, orm.KeyExpr(models.Users.ID)))))), expr.Param(orm.NumberExpr(u.Age), 1)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
		// The only CTE reference appears inside the GROUP BY key.
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(expr.Count[models.User]())), expr.Group(key)), expr.AscValue(expr.Count[models.User]()))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("group key dependency discovery", got, err)
		}
		orders := rowset.As[purchaseAlias](models.Orders.Query(), "purchase")
		link := rowset.Correlate(rowset.From(base), orders)
		parent := rowset.OuterScope(link, rowset.ModelScope(base))
		child := rowset.InnerScope(link, rowset.Scope(orders))
		innerKey := expr.When(expr.Gt(expr.OrderRow(expr.At(child, orm.NumberExpr(models.Orders.TotalCents))), 1), expr.ParamOf(expr.AsSelected(expr.Row(expr.At(child, orm.NumberExpr(models.Orders.TotalCents)))), 1)).Else(expr.ParamOf(expr.AsSelected(expr.Row(expr.At(child, orm.NumberExpr(models.Orders.TotalCents)))), 0))
		selected := rowset.OrderCorrelatedScalar(rowset.DistinctOnCorrelatedScalarValues(rowset.GroupByCorrelatedScalar(rowset.SelectCorrelatedScalar(link, expr.AsSelected(innerKey)).Where(expr.Equal(expr.At(child, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(parent, orm.KeyExpr(models.Users.ID)))), expr.Group(innerKey)), expr.Key(expr.AsSelected(innerKey))), expr.Desc(innerKey)).Limit(1)
		if got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(base), expr.AsSelected(rowset.CorrelatedScalarRowQuery(selected))), expr.Asc(expr.Value(orm.NumberExpr(u.Age))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Null[int64]()}) {
			t.Fatal("correlated computed keys", got, err)
		}
		// Projected computed keys become ordinary fields for the next query scope.
		grouped := rowset.AsRecord[groupedAlias](rowset.CTE("buckets", rowset.RecordOf(buckets())), "counts")
		output := rowset.Scope(grouped)
		if got, err := rowset.ReadScalar(rowset.WhereScalar(rowset.SelectScalar(grouped, expr.AsSelected(expr.Row(rowset.At(output, keyqueries.BucketCounts.Count)))), expr.Eq(rowset.At(output, keyqueries.BucketCounts.Bucket), 1))).RequireFirst(t.Context(), tx); err != nil || got != 2 {
			t.Fatal("computed grouped output field", got, err)
		}
		return nil
	})
}
