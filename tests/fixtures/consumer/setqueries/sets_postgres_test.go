package setqueries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type resultAlias struct{}
type userAlias struct{}

func TestPostgresTypedSets(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		if err := checkValueSets(t, tx); err != nil {
			return err
		}
		if err := checkRecordSets(t, tx, users); err != nil {
			return err
		}
		return checkSetComposition(t, tx, users)
	})
}

func checkValueSets(t *testing.T, tx *database.Tx) error {
	u := models.UserFields()
	ages := query.SelectValue(models.QueryUsers(), u.Age.Value())
	left := ages.Where(u.Age.Lte(21)).UnionAll(ages.Where(u.Age.Eq(21)))  // 20,21,21
	right := ages.Where(u.Age.Gte(21)).UnionAll(ages.Where(u.Age.Eq(22))) // 21,22,22
	for _, test := range []struct {
		name string
		q    query.ValueSetQuery[int]
		want []int
	}{
		{"union", left.Union(right), []int{20, 21, 22}},
		{"union all", left.UnionAll(right), []int{20, 21, 21, 21, 22, 22}},
		{"intersect", left.Intersect(right), []int{21}},
		{"intersect all", left.IntersectAll(right), []int{21}},
		{"except", left.Except(right), []int{20}},
		{"except all", left.ExceptAll(right), []int{20, 21}},
	} {
		q := test.q.OrderBy(test.q.Value().Asc())
		rows, err := q.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rows, test.want) {
			t.Errorf("%s multiplicity: got %v want %v", test.name, rows, test.want)
		}
		if n, err := q.Count(t.Context(), tx); err != nil || n != int64(len(test.want)) {
			t.Error("set count discarded duplicate semantics", test.name, err)
		}
	}
	combined := left.UnionAll(right)
	window := combined.OrderBy(combined.Value().Desc()).Offset(1).Limit(2)
	if rows, err := window.All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{22, 21}) {
		t.Error("combined window mismatch", rows, err)
	}
	if n, err := window.Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("set count ignored selected window", err)
	}
	if rows, err := query.SelectValue(window, window.Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{22, 21}) {
		t.Error("projection of combined value failed", rows, err)
	}
	scalars, err := query.SelectValue(models.QueryUsers(), query.ScalarQuery(models.QueryUsers(), window.Limit(1))).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(scalars) != 3 {
		t.Fatal("set scalar changed outer cardinality")
	}
	for _, v := range scalars {
		if age, ok := v.Get(); !ok || age != 22 {
			t.Error("set scalar lost its concrete value")
		}
	}
	if row, err := window.First(t.Context(), tx); err != nil {
		return err
	} else if age, ok := row.Get(); !ok || age != 22 {
		t.Error("set first ignored ordering/offset")
	}
	if row, err := window.Limit(0).First(t.Context(), tx); err != nil || row.IsSet() {
		t.Error("set first ignored zero limit", err)
	}
	if _, err := window.Limit(0).RequireFirst(t.Context(), tx); !errors.Is(err, database.NotFound) {
		t.Error("empty required set result succeeded", err)
	}
	if exists, err := window.Limit(0).Exists(t.Context(), tx); err != nil || exists {
		t.Error("set existence ignored zero window", err)
	}
	inputWindows := ages.OrderBy(u.Age.Desc()).Limit(1).UnionAll(ages.OrderBy(u.Age.Asc()).Limit(1))
	if rows, err := inputWindows.OrderBy(inputWindows.Value().Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{20, 22}) {
		t.Error("input windows moved after union", rows, err)
	}
	nicknames := query.SelectValue(models.QueryUsers(), u.Nickname.Value())
	for _, test := range []struct {
		q    query.ValueSetQuery[value.Nullable[string]]
		want int64
	}{
		{nicknames.Union(nicknames), 2}, {nicknames.UnionAll(nicknames), 6},
		{nicknames.IntersectAll(nicknames), 3}, {nicknames.ExceptAll(nicknames.Limit(0)), 3},
	} {
		if n, err := test.q.Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("set NULL multiplicity mismatch", n, err)
		}
	}
	if n, err := models.QueryUsers().Where(u.Age.InQuery(window)).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("single-value set lost membership type", err)
	}
	return nil
}

func checkRecordSets(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.UserFields(), models.OrderFields()
	combined := models.QueryUsers().Where(u.Age.Lte(21)).Union(models.QueryUsers().Where(u.Age.Gte(21)))
	f := models.UserFieldsAt(combined.Scope())
	rows, err := combined.Where(f.Status.Eq(models.StatusActive)).OrderBy(f.Email.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 || rows[0].Email != users[0].Email || rows[0].Orders.IsLoaded() {
		t.Fatal("model set lost complete decoding or hydrated relations implicitly")
	}
	if n, err := combined.Where(f.Age.Gte(21)).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("combined model predicate mismatch", err)
	}
	child := query.As[resultAlias](models.QueryOrders(), "child")
	correlation := query.Correlate(combined, child)
	parent := models.UserFieldsAt(query.OuterScope(correlation, combined.Scope()))
	order := models.OrderFieldsAt(query.InnerScope(correlation, child.Scope()))
	if n, err := combined.Where(correlation.Where(order.BuyerID.EqColumn(parent.ID)).Exists()).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("correlation over combined records failed", err)
	}
	// Both reports return BuyerTotals while selecting from different model scopes.
	orders := reports.ProjectBuyerTotals(models.QueryOrders()).SelectBuyerID(o.BuyerID.Value()).SelectTotal(o.TotalCents.Sum().Value()).SelectOrders(query.Count[models.Order]().Value()).SelectAverage(o.TotalCents.Avg().Value()).Query().GroupBy(o.BuyerID.Group())
	people := reports.ProjectBuyerTotals(models.QueryUsers()).SelectBuyerID(u.ID.Value()).SelectTotal(u.Age.Sum().Value()).SelectOrders(query.Count[models.User]().Value()).SelectAverage(u.Age.Avg().Value()).Query().GroupBy(u.ID.Group())
	reportsQuery := orders.UnionAll(people)
	rf := reports.BuyerTotalsFieldsAt(reportsQuery.Scope())
	results, err := reportsQuery.Where(rf.Total.Gte(decimal.FromInt64(20))).OrderBy(rf.Total.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(results) != 3 {
		t.Fatal("set of different input scopes lost output filtering")
	}
	if amount, ok := results[0].Total.Get(); !ok || amount.String() != "20" {
		t.Error("set changed exact decimal codec")
	}
	// Project and group the combined result through the same projection runtime.
	summary := reports.ProjectOrderSummary(reportsQuery).SelectTotal(rf.Total.Sum().Value()).SelectCount(query.Count[query.Set[reports.BuyerTotals]]().Value()).Query()
	row, err := summary.RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	if row.Count != 5 {
		t.Error("projection over set lost rows")
	}
	if total, ok := row.Total.Get(); !ok || total.String() != "69" {
		t.Error("projection over set lost exact aggregate")
	}
	return nil
}

func checkSetComposition(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.UserFields(), models.OrderFields()
	definition := query.CTE("eligible_users", models.QueryUsers().Where(u.Age.Gte(21))).Materialized()
	combined := query.Union(definition, definition)
	statement, err := combined.Compile()
	if err != nil {
		return err
	}
	if strings.Count(statement.SQL(), `FROM "users"`) != 1 {
		t.Fatal("set arms duplicated shared CTE")
	}
	result := query.CTE("combined_users", combined)
	a := query.As[resultAlias](result, "combined")
	b := query.As[userAlias](models.QueryUsers(), "original")
	joined := query.InnerJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).ID, models.UserFieldsAt(b.Scope()).ID))
	fields := models.UserFieldsAt(query.LeftScope(joined, a.Scope()))
	ids := query.SelectValue(joined, fields.ID.Value())
	if n, err := ids.Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("CTE/set join mismatch", err)
	}
	if n, err := models.QueryUsers().WhereHas(models.UserRelations().Referrals.Where(u.ID.InQuery(ids))).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("relation filter lost nested set/CTE", err)
	}
	buyers := query.SelectValue(models.QueryOrders(), o.BuyerID.Value())
	filtered := buyers.Where(o.TotalCents.Lt(3)).Union(buyers.Where(o.TotalCents.Gte(3)))
	if changed, err := models.QueryUsers().Where(u.ID.InQuery(filtered)).Update(t.Context(), tx, users[0].ID, models.UserDraft{}.SetAge(30)); err != nil || changed.Age != 30 {
		t.Error("set subquery failed to constrain typed update", err)
	}
	stop := errors.New("stop set stream")
	if err := combined.Each(t.Context(), tx, func(models.User) error { return stop }); !errors.Is(err, stop) {
		t.Error("set stream lost callback error", err)
	}
	if n, err := combined.Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("set stream failed to release rows", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := combined.All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled set reached executor", err)
	}
	// Deliberately corrupt isolated data to check all-or-error decoding.
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, users[2].ID.String()); err != nil {
		return err
	}
	fieldsAt := models.UserFieldsAt(combined.Scope())
	if rows, err := combined.OrderBy(fieldsAt.Email.Asc()).All(t.Context(), tx); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("set exposed partial malformed models", err)
	}
	return nil
}

func TestInvalidSetsNeverExecute(t *testing.T) {
	ages := query.SelectValue(models.QueryUsers(), models.UserFields().Age.Value())
	combined := ages.Union(ages)
	if rows, err := query.SelectValue(combined, (query.ValueSetQuery[int]{}).Value()).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("zero set expression reached executor", err)
	}
	for _, q := range []query.SetQuery[models.User]{
		{}, models.QueryUsers().Union(models.QueryUsers().With(models.UserRelations().Orders)),
		models.QueryUsers().Union(models.QueryUsers().Where(models.UserFields().Status.Eq(models.Status("bad")))),
		models.QueryUsers().Union(models.QueryUsers()).Limit(-1),
	} {
		if rows, err := q.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid set reached executor", err)
		}
	}
}
