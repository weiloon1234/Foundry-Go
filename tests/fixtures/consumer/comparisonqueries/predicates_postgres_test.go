package comparisonqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/value"

	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresRowValueComparisons(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		f := models.Users
		nextAge := expr.Add(expr.Value(orm.NumberExpr(f.Age)), expr.Param(orm.NumberExpr(f.Age), 1))
		for _, test := range []struct {
			name      string
			condition expr.Predicate[models.User]
			ages      []int
		}{
			{"equal", expr.Equal(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{21}},
			{"not equal", expr.NotEqual(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{20, 22}},
			{"less", expr.Less(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{20}},
			{"less equal", expr.LessOrEqual(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{20, 21}},
			{"greater", expr.Greater(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{22}},
			{"greater equal", expr.GreaterOrEqual(nextAge, expr.Param(orm.NumberExpr(f.Age), 22)), []int{21, 22}},
			{"text pattern", expr.Like(expr.Lower(expr.Value(orm.TextExpr(f.Email))), expr.Param(orm.TextExpr(f.Email), "a%@example.test")), []int{20}},
			{"nullable pattern", expr.LikeNullable(expr.LowerNullable(expr.Value(orm.NullableTextExpr(f.Nickname))), expr.NullableRow(expr.Param(orm.TextExpr(f.Email), "b%"))), []int{21}},
			{"null equality is unknown", expr.Equal(expr.Value(orm.NullableTextExpr(f.Nickname)), expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(f.Email))), value.Null[string]())), []int{}},
			{"null equal", expr.NotDistinctFrom(expr.Value(orm.NullableTextExpr(f.Nickname)), expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(f.Email))), value.Null[string]())), []int{20, 22}},
			{"null distinct", expr.DistinctFrom(expr.Value(orm.NullableTextExpr(f.Nickname)), expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(f.Email))), value.Null[string]())), []int{21}},
			{"nullable range", expr.LessNullable(expr.NullableRow(expr.Value(orm.NumberExpr(f.Age))), expr.NullableRow(expr.Param(orm.NumberExpr(f.Age), 22))), []int{20, 21}},
		} {
			rows, err := orm.All(t.Context(), tx, expr.Where(models.Users.Query(), test.condition).OrderBy(orm.NumberAsc(f.Age)))
			if err != nil {
				t.Fatal(test.name, err)
			}
			ages := make([]int, len(rows))
			for i, row := range rows {
				ages[i] = row.Age
			}
			if !reflect.DeepEqual(ages, test.ages) {
				t.Fatal(test.name, ages, test.ages)
			}
		}
		// Binary conditions also compose with mutation filters and conditional values.
		row, err := orm.Require(t.Context(), tx, expr.Where(models.Users.Query(), expr.Equal(nextAge, expr.Param(orm.NumberExpr(f.Age), 21))))
		if err != nil {
			return err
		}
		_, err = orm.Update(t.Context(), tx, expr.Where(models.Users.Query(), expr.Equal(nextAge, expr.Param(orm.NumberExpr(f.Age), 21))), f.Key(), row.ID, orm.NumberSet(f.Age, 25))
		return err
	})
}

func TestPostgresSelectedValueComparisons(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		q := models.Users.Query()
		f := models.Users
		count := expr.Count[models.User]()
		for _, condition := range []expr.HavingPredicate[models.User]{
			expr.GreaterValue(expr.AsSelected(count), expr.AsSelected(expr.ParamOf(count, 2))),
			expr.LessValue(expr.AsSelected(expr.ParamOf(count, 2)), expr.AsSelected(count)),
			expr.NotDistinctFromValue(expr.AsSelected(count), expr.AsSelected(count)),
		} {
			rows, err := rowset.ReadScalar(rowset.HavingScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(count)), condition)).All(t.Context(), tx)
			if err != nil || !reflect.DeepEqual(rows, []int64{3}) {
				t.Fatal(rows, err)
			}
		}
		email := expr.LowerValue(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(f.Email)))))
		rows, err := rowset.ReadScalar(rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), email), expr.Group(expr.Value(orm.TextExpr(f.Email)))), expr.LikeValue(email, expr.AsSelected(expr.Param(orm.TextExpr(f.Email), "a%"))))).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(rows, []string{"a@example.test"}) {
			t.Fatal(rows, err)
		}
		nickname := expr.LowerNullableValue(expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(f.Nickname)))))
		nullable, err := rowset.ReadScalar(rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), nickname), expr.Group(expr.Value(orm.NullableTextExpr(f.Nickname)))), expr.LikeNullableValue(nickname, expr.Nullable(expr.AsSelected(expr.Param(orm.TextExpr(f.Email), "b%")))))).All(t.Context(), tx)
		if err != nil || len(nullable) != 1 {
			t.Fatal(nullable, err)
		}
		if got, present := nullable[0].Get(); !present || got != "bee" {
			t.Fatal(nullable)
		}
		sum := expr.Sum(orm.NumberExpr(f.Age))
		totals, err := rowset.ReadScalar(rowset.HavingScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(sum)), expr.GreaterNullableValue(expr.AsSelected(sum), expr.Nullable(expr.AsSelected(expr.ParamOf(sum, decimal.FromInt64(60))))))).All(t.Context(), tx)
		if err != nil || len(totals) != 1 {
			t.Fatal(totals, err)
		}
		if got, present := totals[0].Get(); !present || got.String() != "63" {
			t.Fatal(totals)
		}
		return nil
	})
}

func TestComparisonFailuresBeforeExecution(t *testing.T) {
	q := models.Users.Query()
	f := models.Users
	orders := models.Orders.Query()
	link := rowset.Correlate(rowset.From(q), rowset.From(orders))
	outer := rowset.OuterScope(link, rowset.ModelScope(q))
	outerOnly := rowset.CorrelatedScalarRowQuery(rowset.SelectCorrelatedScalar(link, expr.AsSelected(expr.CountFieldAt(outer, orm.KeyExpr(f.ID)))))
	for _, condition := range []expr.Predicate[models.User]{
		expr.Equal[models.User, int](nil, expr.Value(orm.NumberExpr(f.Age))),
		expr.Equal(expr.Value(orm.TextExpr(f.Email)), expr.Param(orm.TextExpr(f.Email), "\x00")),
		expr.Equal(expr.NullableRow(expr.Value(orm.NumberExpr(f.Age))), rowset.ScalarRowQuery(rowset.From(q), rowset.Scalar[models.User, int]{})),
		expr.IsNull(rowset.ScalarRowQuery(rowset.From(q), rowset.SelectScalar(rowset.From(q), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(f.Nickname))))))),
		expr.Equal(outerOnly, expr.NullableRow(expr.ParamOf(expr.Count[models.User](), 1))),
	} {
		if _, err := orm.All(t.Context(), queryfixture.NoQueries(t), expr.Where(q.Limit(0), condition)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
