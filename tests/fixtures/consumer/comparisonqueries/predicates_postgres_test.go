package comparisonqueries_test

import (
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresRowValueComparisons(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		f := models.UserFields()
		nextAge := query.Add(f.Age, f.Age.Param(1))
		for _, test := range []struct {
			name      string
			condition query.Predicate[models.User]
			ages      []int
		}{
			{"equal", query.Equal(nextAge, f.Age.Param(22)), []int{21}},
			{"not equal", query.NotEqual(nextAge, f.Age.Param(22)), []int{20, 22}},
			{"less", query.Less(nextAge, f.Age.Param(22)), []int{20}},
			{"less equal", query.LessOrEqual(nextAge, f.Age.Param(22)), []int{20, 21}},
			{"greater", query.Greater(nextAge, f.Age.Param(22)), []int{22}},
			{"greater equal", query.GreaterOrEqual(nextAge, f.Age.Param(22)), []int{21, 22}},
			{"text pattern", query.Like(query.Lower(f.Email), f.Email.Param("a%@example.test")), []int{20}},
			{"nullable pattern", query.LikeNullable(query.LowerNullable(f.Nickname), query.NullableRow(f.Email.Param("b%"))), []int{21}},
			{"null equality is unknown", query.Equal(f.Nickname, query.NullFor(f.Email)), []int{}},
			{"null equal", query.NotDistinctFrom(f.Nickname, query.NullFor(f.Email)), []int{20, 22}},
			{"null distinct", query.DistinctFrom(f.Nickname, query.NullFor(f.Email)), []int{21}},
			{"nullable range", query.LessNullable(query.NullableRow(f.Age), query.NullableRow(f.Age.Param(22))), []int{20, 21}},
		} {
			rows, err := models.QueryUsers().Where(test.condition).OrderBy(f.Age.Asc()).All(t.Context(), tx)
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
		row, err := models.QueryUsers().Where(query.Equal(nextAge, f.Age.Param(21))).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		_, err = models.QueryUsers().Where(query.Equal(nextAge, f.Age.Param(21))).Update(t.Context(), tx, row.ID, models.UserDraft{}.SetAge(25))
		return err
	})
}

func TestPostgresSelectedValueComparisons(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		q := models.QueryUsers()
		f := models.UserFields()
		count := query.Count[models.User]()
		for _, condition := range []query.HavingPredicate[models.User]{
			query.GreaterValue(count.Value(), count.Param(2).Value()),
			query.LessValue(count.Param(2).Value(), count.Value()),
			query.NotDistinctFromValue(count.Value(), count.Value()),
		} {
			rows, err := query.SelectValue(q, count.Value()).Having(condition).All(t.Context(), tx)
			if err != nil || !reflect.DeepEqual(rows, []int64{3}) {
				t.Fatal(rows, err)
			}
		}
		email := query.LowerValue(f.Email.Value())
		rows, err := query.SelectValue(q, email).GroupBy(f.Email.Group()).Having(query.LikeValue(email, f.Email.Param("a%").Value())).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(rows, []string{"a@example.test"}) {
			t.Fatal(rows, err)
		}
		nickname := query.LowerNullableValue(f.Nickname.Value())
		nullable, err := query.SelectValue(q, nickname).GroupBy(f.Nickname.Group()).Having(query.LikeNullableValue(nickname, query.Nullable(f.Email.Param("b%").Value()))).All(t.Context(), tx)
		if err != nil || len(nullable) != 1 {
			t.Fatal(nullable, err)
		}
		if got, present := nullable[0].Get(); !present || got != "bee" {
			t.Fatal(nullable)
		}
		sum := f.Age.Sum()
		totals, err := query.SelectValue(q, sum.Value()).Having(query.GreaterNullableValue(sum.Value(), query.Nullable(sum.Param(decimal.FromInt64(60)).Value()))).All(t.Context(), tx)
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
	q := models.QueryUsers()
	f := models.UserFields()
	link := query.Correlate(q, models.QueryOrders())
	outer := models.UserFieldsAt(query.OuterScope(link, q.Scope()))
	outerOnly := query.CorrelatedScalarRowQuery(query.SelectCorrelatedValue(link, outer.ID.Count().Value()))
	for _, condition := range []query.Predicate[models.User]{
		query.Equal[models.User, int](nil, f.Age),
		query.Equal(f.Email, f.Email.Param("\x00")),
		query.Equal(query.NullableRow(f.Age), query.ScalarRowQuery[models.User, int](q, nil)),
		query.ScalarRowQuery(q, query.SelectValue(q, f.Nickname.Value())).IsNull(),
		query.Equal(outerOnly, query.NullableRow(query.Count[models.User]().Param(1))),
	} {
		if _, err := q.Limit(0).Where(condition).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
