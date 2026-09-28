package keyqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresComputedGroupKeys(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		base := models.QueryUsers()
		u := models.UserFields()
		bucket := query.When(u.Age.Gt(20), u.Age.Param(1)).Else(u.Age.Param(0))
		if got, err := buckets().All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []keyqueries.BucketCount{{Bucket: 0, Count: 1}, {Bucket: 1, Count: 2}}) {
			t.Fatal("computed group hydration or parameter identity", got, err)
		}
		if got, err := buckets().Having(query.Grouped(bucket.Eq(1))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []keyqueries.BucketCount{{Bucket: 1, Count: 2}}) {
			t.Fatal("computed group HAVING", got, err)
		}
		// A larger selected key must reuse its grouped child's parameter IDs.
		nested := query.NullIf(bucket, u.Age.Param(1))
		q := query.SelectValue(base, nested.Value()).GroupBy(bucket.Group()).DistinctOnValues(nested.Value().Key()).OrderBy(nested.Asc())
		if got, err := q.All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Of(0), value.Null[int]()}) {
			t.Fatal("grouped child inside selected DISTINCT key", got, err)
		}
		if got, err := query.SelectValue(base, nested.Value()).GroupBy(bucket.Group()).Distinct().OrderBy(nested.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Of(0), value.Null[int]()}) {
			t.Fatal("ordinary DISTINCT with grouped parameters", got, err)
		}
		constant := u.Age.Param(1)
		if got, err := query.SelectValue(base, query.Count[models.User]().Value()).GroupBy(constant.Group()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{3}) {
			t.Fatal("integer parameter treated as GROUP BY ordinal", got, err)
		}
		// NULL and an empty text value form different groups.
		nickname := query.When(u.Age.Eq(20), u.Nickname.Param("")).ElseNull()
		if got, err := query.SelectValue(base, query.Count[models.User]().Value()).GroupBy(nickname.Group()).OrderBy(nickname.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("nullable group lost empty value", got, err)
		}
		// An empty membership operand is compiled for validation then discarded.
		choice := query.When(constant.In(), u.Age.Param(9)).Else(u.Age.Param(7))
		if got, err := query.SelectValue(base, choice.Value()).GroupBy(constant.Group()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{7}) {
			t.Fatal("empty membership retained discarded key bindings", got, err)
		}
		return nil
	})
}

func TestPostgresComputedPartitionsAndDistinct(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base := models.QueryUsers()
		u := models.UserFields()
		bucket := query.When(u.Age.Gt(20), u.Age.Param(1)).Else(u.Age.Param(0))
		partition := query.WindowFor(base).PartitionBy(bucket.Group()).OrderBy(u.Age.Asc())
		rank := query.RowNumber(partition)
		if got, err := query.SelectValue(base, rank).OrderBy(u.Age.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1, 2}) {
			t.Fatal("computed row partition", got, err)
		}
		if got, err := base.DistinctOn(bucket.Group()).OrderBy(bucket.Asc(), u.Age.Desc()).All(t.Context(), tx); err != nil || len(got) != 2 || got[0].ID != users[0].ID || got[1].ID != users[2].ID {
			t.Fatal("computed DISTINCT ON precedence", got, err)
		}
		// Window outputs are evaluated before DISTINCT ON.
		if got, err := query.SelectValue(base, u.Age.Value()).DistinctOnValues(rank.Key()).OrderBy(rank.Asc(), u.Age.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 22}) {
			t.Fatal("window DISTINCT key", got, err)
		}
		count := query.Count[models.User]()
		groupedWindow := query.WindowFor(base).PartitionByValues(count.Value().Key())
		if got, err := query.SelectValue(base, query.RowNumber(groupedWindow)).GroupBy(bucket.Group()).OrderBy(count.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("aggregate partition over grouped rows", got, err)
		}
		if got, err := query.SelectValue(base, count.Value()).GroupBy(bucket.Group()).DistinctOnValues(count.Value().Key()).OrderBy(count.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("aggregate DISTINCT key", got, err)
		}
		combined := query.SelectValue(base, u.Age.Value()).UnionAll(query.SelectValue(base, u.Age.Value()))
		if got, err := combined.DistinctOnValues(combined.Value().Key()).OrderBy(combined.Value().Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{20, 21, 22}) {
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
		base := models.QueryUsers()
		u := models.UserFields()
		eligible := query.As[eligibleAlias](query.CTE("eligible", base.Where(u.Age.Gt(20))), "eligible_users")
		f := models.UserFieldsAt(eligible.Scope())
		key := query.When(u.ID.InQuery(query.SelectValue(eligible, f.ID.Value())), u.Age.Param(1)).Else(u.Age.Param(0))
		// The only CTE reference appears inside the GROUP BY key.
		if got, err := query.SelectValue(base, query.Count[models.User]().Value()).GroupBy(key.Group()).OrderBy(query.Count[models.User]().Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Fatal("group key dependency discovery", got, err)
		}
		orders := query.As[purchaseAlias](models.QueryOrders(), "purchase")
		link := query.Correlate(base, orders)
		parent := models.UserFieldsAt(query.OuterScope(link, base.Scope()))
		child := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		innerKey := query.When(child.TotalCents.Gt(1), child.TotalCents.Param(1)).Else(child.TotalCents.Param(0))
		selected := query.SelectCorrelatedValue(link, innerKey.Value()).Where(child.BuyerID.EqColumn(parent.ID)).GroupBy(innerKey.Group()).DistinctOnValues(innerKey.Value().Key()).OrderBy(innerKey.Desc()).Limit(1)
		if got, err := query.SelectValue(base, query.CorrelatedScalarQuery(selected)).OrderBy(u.Age.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of(int64(1)), value.Of(int64(1)), value.Null[int64]()}) {
			t.Fatal("correlated computed keys", got, err)
		}
		// Projected computed keys become ordinary fields for the next query scope.
		grouped := query.As[groupedAlias](query.CTE("buckets", buckets()), "counts")
		output := keyqueries.BucketCountFieldsAt(grouped.Scope())
		if got, err := query.SelectValue(grouped, output.Count.Value()).Where(output.Bucket.Eq(1)).RequireFirst(t.Context(), tx); err != nil || got != 2 {
			t.Fatal("computed grouped output field", got, err)
		}
		return nil
	})
}
