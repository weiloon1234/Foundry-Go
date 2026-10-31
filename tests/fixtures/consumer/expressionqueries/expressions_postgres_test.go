package expressionqueries_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresRowConditionalResults(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.Users
		base := models.Users.Query().OrderBy(orm.TextAsc(u.Email))
		display := expr.Coalesce(expr.Value(orm.NullableTextExpr(u.Nickname)), expr.Value(orm.TextExpr(u.Email)))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.AsSelected(display))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{users[0].Email, "Bee", users[2].Email}) {
			t.Fatal("coalesce fallback", got, err)
		}
		if got, err := rows.Read(rows.FromModel(expr.Where(base, expr.Eq(display, "Bee")))).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("computed row predicate", err)
		}
		label := expr.When(expr.ModelPredicate(orm.NumberEq(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 2)).When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 10)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
		// All result branches are parameters: PostgreSQL must compare numbers, not text.
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(label))), expr.Asc(label))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{2, 10}) {
			t.Fatal("CASE parameters lost numeric type", got, err)
		}
		if got, err := orm.Count(t.Context(), tx, expr.Where(base, expr.In(label))); err != nil || got != 0 {
			t.Fatal("empty computed IN", got, err)
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(base, expr.Not(expr.In(label)), expr.ModelPredicate(orm.NumberEq(u.Age, 21)))), expr.AsSelected(expr.Param(orm.TextExpr(u.Email), "kept")))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatal("empty IN disrupted surrounding bindings", got, err)
		}
		first := expr.When(expr.ModelPredicate(orm.NumberGte(u.Age, 20)), expr.Param(orm.TextExpr(u.Email), "first")).When(expr.ModelPredicate(orm.NumberGte(u.Age, 21)), expr.Param(orm.TextExpr(u.Email), "later")).Else(expr.Param(orm.TextExpr(u.Email), "none"))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.AsSelected(first))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"first", "first", "first"}) {
			t.Fatal("CASE branch order", got, err)
		}
		optional := expr.When(expr.ModelPredicate(orm.NullableTextLike(u.Nickname, "B%")), expr.Value(orm.NumberExpr(u.Age))).ElseNull()
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.AsSelected(optional))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Null[int](), value.Of(21), value.Null[int]()}) {
			t.Fatal("CASE unknown condition/ELSE NULL", got, err)
		}
		removed := expr.NullIf(display, expr.Param(orm.TextExpr(u.Email), "Bee"))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.AsSelected(removed))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Of(users[0].Email), value.Null[string](), value.Of(users[2].Email)}) {
			t.Fatal("NULLIF semantics", got, err)
		}
		if got, err := rows.Read(rows.FromModel(expr.Where(base, expr.IsNull(removed)))).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("nullable computed predicate", err)
		}
		nullable := expr.NullIfNullable(expr.Value(orm.NullableTextExpr(u.Nickname)), expr.NullableRow(expr.Param(orm.TextExpr(u.Email), "Bee")))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.AsSelected(expr.CoalesceNullable(nullable, expr.ParamOf(expr.NullableRow(expr.Value(orm.TextExpr(u.Email))), value.Null[string]()))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Null[string](), value.Null[string](), value.Null[string]()}) {
			t.Fatal("nullable result layers", got, err)
		}
		if _, err := orm.Update(t.Context(), tx, models.Users.Table.Query(), models.Users.Table.Key(), users[0].ID, orm.NullableTextSet(models.Users.Nickname, "")); err != nil {
			return err
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(base, expr.ModelPredicate(orm.KeyEq(u.ID, users[0].ID)))), expr.AsSelected(display))).RequireFirst(t.Context(), tx); err != nil || got != "" {
			t.Fatal("coalesce replaced an empty string", got, err)
		}
		return nil
	})
}

func TestPostgresSelectedConditionalValues(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.Orders
		orders := models.Orders.Query()
		total := expr.Sum(orm.NumberExpr(o.TotalCents))
		fallback := expr.CoalesceValue(expr.AsSelected(total), expr.AsSelected(expr.ParamOf(total, decimal.FromInt64(0))))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(orders.Where(orm.NumberLt(o.TotalCents, 0))), fallback)).RequireFirst(t.Context(), tx); err != nil || got != decimal.FromInt64(0) {
			t.Fatal("empty aggregate fallback", got, err)
		}
		count := expr.Count[models.Order]()
		selected := expr.WhenValue(expr.GtValue(count, 1), expr.AsSelected(count)).Else(expr.AsSelected(expr.ParamOf(count, 0)))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(orders), selected), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.DescValue(count))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{2, 0}) {
			t.Fatal("CASE of grouped aggregates", got, err)
		}
		w := expr.WindowFor(rows.ModelScope(orders)).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))))
		number := expr.RowNumber(w)
		windowValue := expr.WhenValue(expr.Grouped(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 1))), number).Else(expr.AsSelected(expr.ParamOf(count, 0)))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(orders), windowValue), expr.Asc(expr.Value(orm.NumberExpr(o.TotalCents))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{0, 2, 3}) {
			t.Fatal("conditional window result", got, err)
		}
		// A window may consume an ordinary conditional value over grouped counts.
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(orders), expr.LagOr(selected, 1, int64(-1), expr.WindowFor(rows.ModelScope(orders)).OrderBy(expr.DescValue(count)))), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.DescValue(count))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{-1, 2}) {
			t.Fatal("conditional aggregate did not establish window input grouping", got, err)
		}
		filtered := expr.Filter(expr.Count[models.Order](), expr.Eq(expr.When(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 1)), expr.Param(orm.NumberExpr(o.TotalCents), 1)).Else(expr.Param(orm.NumberExpr(o.TotalCents), 0)), 1))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(orders), expr.AsSelected(filtered))).RequireFirst(t.Context(), tx); err != nil || got != 2 {
			t.Fatal("computed aggregate filter", got, err)
		}
		return nil
	})
}

type selectedAlias struct{}
type orderAlias struct{}

func TestPostgresConditionalCompositionAndFailures(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.Users, models.Orders
		people := models.Users.Query()
		common := rows.AsRecord[selectedAlias](rows.CTE("eligible", rows.ModelRecord(expr.Where(people, expr.ModelPredicate(orm.NumberGt(u.Age, 20))))), "selected")
		f := rows.Scope(common)
		label := expr.When(rows.InQuery(expr.Value(orm.KeyExpr(u.ID)), rows.SelectScalar(common, expr.AsSelected(expr.Row(expr.At(f, orm.KeyExpr(u.ID)))))), expr.Param(orm.TextExpr(u.Email), "yes")).Else(expr.Param(orm.TextExpr(u.Email), "no"))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), expr.AsSelected(label)), expr.Asc(expr.Value(orm.TextExpr(u.Email))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"no", "yes", "yes"}) {
			t.Fatal("conditional CTE discovery", got, err)
		}
		// A selected scalar owns its grouping and discovers dependencies even
		// when the only reference to its CTE is inside a conditional value.
		chosen := rows.LimitScalar(rows.OrderScalar(rows.SelectScalar(common, expr.AsSelected(expr.Row(expr.At(f, orm.NumberExpr(u.Age))))), expr.Desc(expr.At(f, orm.NumberExpr(u.Age)))), 1)
		age := expr.CoalesceValue(expr.AsSelected(rows.ScalarRowQuery(rows.From(people), chosen)), expr.AsSelected(expr.Param(orm.NumberExpr(u.Age), -1)))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), age), expr.Asc(expr.Value(orm.TextExpr(u.Email))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{22, 22, 22}) {
			t.Fatal("conditional scalar/CTE composition", got, err)
		}
		orders := rows.As[orderAlias](models.Orders.Query(), "purchase")
		link := rows.Correlate(rows.From(people), orders)
		parent := rows.OuterScope(link, rows.ModelScope(people))
		child := rows.InnerScope(link, rows.Scope(orders))
		bought := link.Where(expr.Equal(expr.At(child, orm.ColumnExpr(o.BuyerID)), expr.At(parent, orm.KeyExpr(u.ID)))).Exists()
		withOrders := expr.When(bought, expr.Param(orm.TextExpr(u.Email), "yes")).Else(expr.Param(orm.TextExpr(u.Email), "no"))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), expr.AsSelected(withOrders)), expr.Asc(expr.Value(orm.TextExpr(u.Email))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"yes", "yes", "no"}) {
			t.Fatal("conditional correlation", got, err)
		}
		display := expr.Coalesce(expr.Value(orm.NullableTextExpr(u.Nickname)), expr.Value(orm.TextExpr(u.Email)))
		projection := rows.Select(rows.From(people), reports.UserSummaries, rows.MapSelected(reports.UserSummaries.ID, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(u.ID))))), rows.MapSelected(reports.UserSummaries.Email, expr.AsSelected(display)), rows.MapSelected(reports.UserSummaries.Nickname, expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname))))), rows.MapSelected(reports.UserSummaries.Status, expr.AsSelected(expr.Row(expr.Value(orm.EnumExpr(u.Status))))))
		alias := rows.AsRecord[selectedAlias](rows.RecordOf(projection), "displayed")
		fields := rows.At(rows.Scope(alias), reports.UserSummaries.Email)
		if got, err := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(alias, expr.AsSelected(expr.Row(fields))), expr.Eq(fields, "Bee"))).RequireFirst(t.Context(), tx); err != nil || got != "Bee" {
			t.Fatal("typed projected conditional output", got, err)
		}
		r := models.UserLinks
		computation := expr.Filter(expr.Count[models.Order](), expr.Eq(expr.When(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 1)), expr.Param(orm.NumberExpr(o.TotalCents), 1)).Else(expr.Param(orm.NumberExpr(o.TotalCents), 0)), 1))
		slot := models.Users.OrderCount.Using(expr.Related(r.Orders, computation))
		items, err := orm.All(t.Context(), tx, people.With(slot).OrderBy(orm.TextAsc(u.Email)))
		if err != nil {
			return err
		}
		for i, row := range items {
			count, loaded := row.OrderCount.Get()
			if !loaded || count != []int64{1, 1, 0}[i] {
				t.Fatal("conditional relation aggregate", row)
			}
		}
		// Even unreachable parameters must pass their codec before any I/O.
		invalid := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 0)), expr.Value(orm.EnumExpr(u.Status))).Else(expr.Param(orm.EnumExpr(u.Status), models.Status("invalid")))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(people), expr.AsSelected(invalid))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) || got != nil {
			t.Fatal("invalid parameter reached execution", err)
		}
		// The same computed row predicates also constrain model writes.
		updated, err := orm.Update(t.Context(), tx, expr.Where(people, expr.Eq(display, "Bee")), u.Key(), users[1].ID, orm.NullableTextSet(models.Users.Nickname, "B updated"))
		if err != nil || updated.Nickname != value.Of("B updated") {
			t.Fatal("computed update predicate", updated, err)
		}
		return nil
	})
}
