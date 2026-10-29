package setqueries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
	u := models.Users
	ages := rowset.SelectScalar(rowset.From(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(u.Age))).Value())
	left := rowset.UnionAllValues(rowset.Values(rowset.WhereScalar(ages, expr.ModelPredicate(orm.NumberLte(u.Age, 21)))), rowset.Values(rowset.WhereScalar(ages, expr.ModelPredicate(orm.NumberEq(u.Age, 21)))))  // 20,21,21
	right := rowset.UnionAllValues(rowset.Values(rowset.WhereScalar(ages, expr.ModelPredicate(orm.NumberGte(u.Age, 21)))), rowset.Values(rowset.WhereScalar(ages, expr.ModelPredicate(orm.NumberEq(u.Age, 22))))) // 21,22,22
	for _, test := range []struct {
		name string
		q    rowset.ValueSetQuery[int]
		want []int
	}{
		{"union", rowset.UnionValues(left, right), []int{20, 21, 22}},
		{"union all", rowset.UnionAllValues(left, right), []int{20, 21, 21, 21, 22, 22}},
		{"intersect", rowset.IntersectValues(left, right), []int{21}},
		{"intersect all", rowset.IntersectAllValues(left, right), []int{21}},
		{"except", rowset.ExceptValues(left, right), []int{20}},
		{"except all", rowset.ExceptAllValues(left, right), []int{20, 21}},
	} {
		q := test.q.OrderBy(test.q.Value().Asc())
		rows, err := rowset.ReadValues(q).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rows, test.want) {
			t.Errorf("%s multiplicity: got %v want %v", test.name, rows, test.want)
		}
		if n, err := rowset.ReadValues(q).Count(t.Context(), tx); err != nil || n != int64(len(test.want)) {
			t.Error("set count discarded duplicate semantics", test.name, err)
		}
	}
	combined := rowset.UnionAllValues(left, right)
	window := combined.OrderBy(combined.Value().Desc()).Offset(1).Limit(2)
	if rows, err := rowset.ReadValues(window).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{22, 21}) {
		t.Error("combined window mismatch", rows, err)
	}
	if n, err := rowset.ReadValues(window).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("set count ignored selected window", err)
	}
	if rows, err := rowset.ReadScalar(rowset.SelectScalar(window, window.Value().Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{22, 21}) {
		t.Error("projection of combined value failed", rows, err)
	}
	scalars, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.ScalarRowQuery(rowset.From(models.Users.Query()), rowset.SelectScalar(window.Limit(1), window.Value().Value())).Value())).All(t.Context(), tx)
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
	if row, err := rowset.ReadValues(window).First(t.Context(), tx); err != nil {
		return err
	} else if age, ok := row.Get(); !ok || age != 22 {
		t.Error("set first ignored ordering/offset")
	}
	if row, err := rowset.ReadValues(window.Limit(0)).First(t.Context(), tx); err != nil || row.IsSet() {
		t.Error("set first ignored zero limit", err)
	}
	if _, err := rowset.ReadValues(window.Limit(0)).RequireFirst(t.Context(), tx); !errors.Is(err, database.NotFound) {
		t.Error("empty required set result succeeded", err)
	}
	if exists, err := rowset.ReadValues(window.Limit(0)).Exists(t.Context(), tx); err != nil || exists {
		t.Error("set existence ignored zero window", err)
	}
	inputWindows := rowset.UnionAllValues(rowset.Values(rowset.LimitScalar(rowset.OrderScalar(ages, expr.Value(orm.NumberExpr(u.Age)).Desc()), 1)), rowset.Values(rowset.LimitScalar(rowset.OrderScalar(ages, expr.Value(orm.NumberExpr(u.Age)).Asc()), 1)))
	if rows, err := rowset.ReadValues(inputWindows.OrderBy(inputWindows.Value().Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int{20, 22}) {
		t.Error("input windows moved after union", rows, err)
	}
	nicknames := rowset.SelectScalar(rowset.From(models.Users.Query()), expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname))).Value())
	for _, test := range []struct {
		q    rowset.ValueSetQuery[value.Nullable[string]]
		want int64
	}{
		{rowset.UnionValues(rowset.Values(nicknames), rowset.Values(nicknames)), 2}, {rowset.UnionAllValues(rowset.Values(nicknames), rowset.Values(nicknames)), 6},
		{rowset.IntersectAllValues(rowset.Values(nicknames), rowset.Values(nicknames)), 3}, {rowset.ExceptAllValues(rowset.Values(nicknames), rowset.Values(rowset.LimitScalar(nicknames, 0))), 3},
	} {
		if n, err := rowset.ReadValues(test.q).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("set NULL multiplicity mismatch", n, err)
		}
	}
	if n, err := orm.Count(t.Context(), tx, expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(orm.NumberExpr(u.Age)), rowset.SelectScalar(window, window.Value().Value())))); err != nil || n != 2 {
		t.Error("single-value set lost membership type", err)
	}
	return nil
}

func checkSetComposition(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.Users, models.Orders
	definition := rowset.CTE("eligible_users", rowset.ModelRecord(models.Users.Query().Where(orm.NumberGte(u.Age, 21)))).Materialized()
	combined := rowset.Union(definition, definition)
	statement, err := rowset.CompileRead(rowset.ReadSet(combined))
	if err != nil {
		return err
	}
	if strings.Count(statement.SQL(), `FROM "users"`) != 1 {
		t.Fatal("set arms duplicated shared CTE")
	}
	result := rowset.CTE("combined_users", combined)
	a := rowset.AsRecord[resultAlias](result, "combined")
	b := rowset.As[userAlias](models.Users.Query(), "original")
	joined := rowset.InnerJoin(a, b, rowset.On(expr.At(rowset.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rowset.Scope(b), orm.KeyExpr(models.Users.ID))))
	fields := rowset.LeftScope(joined, rowset.Scope(a))
	ids := rowset.SelectScalar(joined, expr.Row(expr.At(fields, orm.KeyExpr(models.Users.ID))).Value())
	if n, err := rowset.ReadScalar(ids).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("CTE/set join mismatch", err)
	}
	if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(orm.Has(models.UserLinks.Referrals.Where(expr.RelationFilter(models.UserLinks.Referrals, rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids)))))); err != nil || n != 1 {
		t.Error("relation filter lost nested set/CTE", err)
	}
	buyers := rowset.SelectScalar(rowset.From(models.Orders.Query()), expr.Row(expr.Value(orm.ColumnExpr(o.BuyerID))).Value())
	filtered := rowset.UnionValues(rowset.Values(rowset.WhereScalar(buyers, expr.ModelPredicate(orm.NumberLt(o.TotalCents, 3)))), rowset.Values(rowset.WhereScalar(buyers, expr.ModelPredicate(orm.NumberGte(o.TotalCents, 3)))))
	if changed, err := orm.Update(t.Context(), tx, expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), rowset.SelectScalar(filtered, filtered.Value().Value()))), u.Key(), users[0].ID, orm.NumberSet(u.Age, 30)); err != nil || changed.Age != 30 {
		t.Error("set subquery failed to constrain typed update", err)
	}
	stop := errors.New("stop set stream")
	if err := rowset.EachRead(t.Context(), tx, rowset.ReadSet(combined), func(models.User) error { return stop }); !errors.Is(err, stop) {
		t.Error("set stream lost callback error", err)
	}
	if n, err := rowset.ReadSet(combined).Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("set stream failed to release rows", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := rowset.ReadSet(combined).All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled set reached executor", err)
	}
	// Deliberately corrupt isolated data to check all-or-error decoding.
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, users[2].ID.String()); err != nil {
		return err
	}
	fieldsAt := combined.Scope()
	if rows, err := rowset.ReadSet(combined.OrderBy(expr.At(fieldsAt, orm.TextExpr(models.Users.Email)).Asc())).All(t.Context(), tx); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("set exposed partial malformed models", err)
	}
	return nil
}

func TestInvalidSetsNeverExecute(t *testing.T) {
	ages := rowset.SelectScalar(rowset.From(models.Users.Query()), expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))).Value())
	combined := rowset.UnionValues(rowset.Values(ages), rowset.Values(ages))
	if rows, err := rowset.ReadScalar(rowset.SelectScalar(combined, (rowset.ValueSetQuery[int]{}).Value().Value())).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("zero set expression reached executor", err)
	}
	for _, q := range []rowset.SetQuery[models.User]{
		{}, rowset.Union(rowset.ModelRecord(models.Users.Query()), rowset.ModelRecord(models.Users.Query().With(models.UserLinks.Orders))),
		rowset.Union(rowset.ModelRecord(models.Users.Query()), rowset.ModelRecord(models.Users.Query().Where(orm.EnumEq(models.Users.Status, models.Status("bad"))))),
		rowset.Union(rowset.ModelRecord(models.Users.Query()), rowset.ModelRecord(models.Users.Query())).Limit(-1),
	} {
		if rows, err := rowset.ReadSet(q).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid set reached executor", err)
		}
	}
}

func checkRecordSets(t *testing.T, tx *database.Tx, users []models.User) error {
	u := models.Users
	combined := rowset.Union(rowset.ModelRecord(models.Users.Query().Where(orm.NumberLte(u.Age, 21))), rowset.ModelRecord(models.Users.Query().Where(orm.NumberGte(u.Age, 21))))
	f := combined.Scope()
	rows, err := rowset.ReadSet(combined.Where(expr.At(f, orm.EnumExpr(models.Users.Status)).Eq(models.StatusActive)).OrderBy(expr.At(f, orm.TextExpr(models.Users.Email)).Asc())).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 || rows[0].Email != users[0].Email || rows[0].Orders.IsLoaded() {
		t.Fatal("model set lost complete decoding or hydrated relations implicitly")
	}
	if n, err := rowset.ReadSet(combined.Where(expr.OrderRow(expr.At(f, orm.NumberExpr(models.Users.Age))).Gte(21))).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("combined model predicate mismatch", err)
	}
	child := rowset.As[resultAlias](models.Orders.Query(), "child")
	parentSource := rowset.AsRecord[userAlias](combined, "combined_parent")
	correlation := rowset.Correlate(parentSource, child)
	parent := rowset.OuterScope(correlation, rowset.Scope(parentSource))
	order := rowset.InnerScope(correlation, rowset.Scope(child))
	if n, err := rowset.ReadRecord(rowset.SelectRecord(parentSource, rowset.Scope(parentSource)).Where(correlation.Where(expr.Equal(expr.At(order, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(parent, orm.KeyExpr(models.Users.ID)))).Exists())).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("correlation over combined records failed", err)
	}
	return checkDerivedSetAggregation(t, tx)
}
