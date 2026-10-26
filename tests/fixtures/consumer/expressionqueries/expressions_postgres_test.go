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
		base := models.Users.Query().OrderBy(u.Email.Asc())
		display := expr.Coalesce(expr.Value(u.Nickname), expr.Value(u.Email))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), display.Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{users[0].Email, "Bee", users[2].Email}) {
			t.Fatal("coalesce fallback", got, err)
		}
		if got, err := rows.Read(rows.FromModel(expr.Where(base, display.Eq("Bee")))).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("computed row predicate", err)
		}
		label := expr.When(expr.ModelPredicate(u.Age.Eq(20)), expr.Param(u.Age, 2)).When(expr.ModelPredicate(u.Age.Gt(20)), expr.Param(u.Age, 10)).Else(expr.Param(u.Age, 0))
		// All result branches are parameters: PostgreSQL must compare numbers, not text.
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), label.Value())), label.Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{2, 10}) {
			t.Fatal("CASE parameters lost numeric type", got, err)
		}
		if got, err := expr.Where(base, label.In()).Count(t.Context(), tx); err != nil || got != 0 {
			t.Fatal("empty computed IN", got, err)
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(base, expr.Not(label.In()), expr.ModelPredicate(u.Age.Eq(21)))), expr.Param(u.Email, "kept").Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatal("empty IN disrupted surrounding bindings", got, err)
		}
		first := expr.When(expr.ModelPredicate(u.Age.Gte(20)), expr.Param(u.Email, "first")).When(expr.ModelPredicate(u.Age.Gte(21)), expr.Param(u.Email, "later")).Else(expr.Param(u.Email, "none"))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), first.Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"first", "first", "first"}) {
			t.Fatal("CASE branch order", got, err)
		}
		optional := expr.When(expr.ModelPredicate(u.Nickname.Like("B%")), expr.Value(u.Age)).ElseNull()
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), optional.Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Null[int](), value.Of(21), value.Null[int]()}) {
			t.Fatal("CASE unknown condition/ELSE NULL", got, err)
		}
		removed := expr.NullIf(display, expr.Param(u.Email, "Bee"))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), removed.Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Of(users[0].Email), value.Null[string](), value.Of(users[2].Email)}) {
			t.Fatal("NULLIF semantics", got, err)
		}
		if got, err := rows.Read(rows.FromModel(expr.Where(base, removed.IsNull()))).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("nullable computed predicate", err)
		}
		nullable := expr.NullIfNullable(expr.Value(u.Nickname), expr.NullableRow(expr.Param(u.Email, "Bee")))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(base), expr.CoalesceNullable(nullable, expr.NullFor(expr.Value(u.Email))).Value())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Null[string](), value.Null[string](), value.Null[string]()}) {
			t.Fatal("nullable result layers", got, err)
		}
		if _, err := models.Users.Update(t.Context(), tx, users[0].ID, models.Users.Nickname.Set("")); err != nil {
			return err
		}
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(base, expr.ModelPredicate(u.ID.Eq(users[0].ID)))), display.Value())).RequireFirst(t.Context(), tx); err != nil || got != "" {
			t.Fatal("coalesce replaced an empty string", got, err)
		}
		return nil
	})
}

func TestPostgresSelectedConditionalValues(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.Orders
		orders := models.Orders.Query()
		total := expr.Sum(o.TotalCents)
		fallback := expr.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(orders.Where(o.TotalCents.Lt(0))), fallback)).RequireFirst(t.Context(), tx); err != nil || got != decimal.FromInt64(0) {
			t.Fatal("empty aggregate fallback", got, err)
		}
		count := expr.Count[models.Order]()
		selected := expr.WhenValue(count.Gt(1), count.Value()).Else(count.Param(0).Value())
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(orders), selected), expr.Group(expr.Value(o.BuyerID))), count.Desc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{2, 0}) {
			t.Fatal("CASE of grouped aggregates", got, err)
		}
		w := expr.WindowFor(rows.ModelScope(orders)).OrderBy(expr.Value(o.TotalCents).Asc())
		number := expr.RowNumber(w)
		windowValue := expr.WhenValue(expr.Grouped(expr.ModelPredicate(o.TotalCents.Gt(1))), number).Else(count.Param(0).Value())
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(orders), windowValue), expr.Value(o.TotalCents).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{0, 2, 3}) {
			t.Fatal("conditional window result", got, err)
		}
		// A window may consume an ordinary conditional value over grouped counts.
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(orders), expr.LagOr(selected, 1, int64(-1), expr.WindowFor(rows.ModelScope(orders)).OrderBy(count.Desc()))), expr.Group(expr.Value(o.BuyerID))), count.Desc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{-1, 2}) {
			t.Fatal("conditional aggregate did not establish window input grouping", got, err)
		}
		filtered := expr.Count[models.Order]().Filter(expr.When(expr.ModelPredicate(o.TotalCents.Gt(1)), expr.Param(o.TotalCents, 1)).Else(expr.Param(o.TotalCents, 0)).Eq(1))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(orders), filtered.Value())).RequireFirst(t.Context(), tx); err != nil || got != 2 {
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
		common := rows.AsRecord[selectedAlias](rows.CTE("eligible", rows.ModelRecord(expr.Where(people, expr.ModelPredicate(u.Age.Gt(20))))), "selected")
		f := rows.Scope(common)
		label := expr.When(rows.InQuery(expr.Value(u.ID), rows.SelectScalar(common, expr.Row(expr.At(f, u.ID)).Value())), expr.Param(u.Email, "yes")).Else(expr.Param(u.Email, "no"))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), label.Value()), expr.Value(u.Email).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"no", "yes", "yes"}) {
			t.Fatal("conditional CTE discovery", got, err)
		}
		// A selected scalar owns its grouping and discovers dependencies even
		// when the only reference to its CTE is inside a conditional value.
		chosen := rows.LimitScalar(rows.OrderScalar(rows.SelectScalar(common, expr.Row(expr.At(f, u.Age)).Value()), expr.At(f, u.Age).Desc()), 1)
		age := expr.CoalesceValue(rows.ScalarQuery(rows.From(people), chosen), expr.Param(u.Age, -1).Value())
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), age), expr.Value(u.Email).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{22, 22, 22}) {
			t.Fatal("conditional scalar/CTE composition", got, err)
		}
		orders := rows.As[orderAlias](models.Orders.Query(), "purchase")
		link := rows.Correlate(rows.From(people), orders)
		parent := rows.OuterScope(link, rows.ModelScope(people))
		child := rows.InnerScope(link, rows.Scope(orders))
		bought := link.Where(expr.Equal(expr.At(child, o.BuyerID), expr.At(parent, u.ID))).Exists()
		withOrders := expr.When(bought, expr.Param(u.Email, "yes")).Else(expr.Param(u.Email, "no"))
		if got, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(people), withOrders.Value()), expr.Value(u.Email).Asc())).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"yes", "yes", "no"}) {
			t.Fatal("conditional correlation", got, err)
		}
		display := expr.Coalesce(expr.Value(u.Nickname), expr.Value(u.Email))
		projection := rows.Select(rows.From(people), reports.UserSummaries, rows.MapSelected(reports.UserSummaries.ID, expr.Row(expr.Value(u.ID)).Value()), rows.MapSelected(reports.UserSummaries.Email, display.Value()), rows.MapSelected(reports.UserSummaries.Nickname, expr.Row(expr.Value(u.Nickname)).Value()), rows.MapSelected(reports.UserSummaries.Status, expr.Row(expr.Value(u.Status)).Value()))
		alias := rows.AsRecord[selectedAlias](rows.RecordOf(projection), "displayed")
		fields := rows.At(rows.Scope(alias), reports.UserSummaries.Email)
		if got, err := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(alias, expr.Row(fields).Value()), fields.Eq("Bee"))).RequireFirst(t.Context(), tx); err != nil || got != "Bee" {
			t.Fatal("typed projected conditional output", got, err)
		}
		r := models.UserLinks
		computation := expr.Count[models.Order]().Filter(expr.When(expr.ModelPredicate(o.TotalCents.Gt(1)), expr.Param(o.TotalCents, 1)).Else(expr.Param(o.TotalCents, 0)).Eq(1))
		slot := models.Users.OrderCount.Using(expr.Related(r.Orders, computation))
		items, err := people.With(slot).OrderBy(u.Email.Asc()).All(t.Context(), tx)
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
		invalid := expr.When(expr.ModelPredicate(u.Age.Gt(0)), expr.Value(u.Status)).Else(expr.Param(u.Status, models.Status("invalid")))
		if got, err := rows.ReadScalar(rows.SelectScalar(rows.From(people), invalid.Value())).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) || got != nil {
			t.Fatal("invalid parameter reached execution", err)
		}
		// The same computed row predicates also constrain model writes.
		updated, err := orm.Update(t.Context(), tx, expr.Where(people, display.Eq("Bee")), u.Key(), users[1].ID, models.Users.Nickname.Set("B updated"))
		if err != nil || updated.Nickname != value.Of("B updated") {
			t.Fatal("computed update predicate", updated, err)
		}
		return nil
	})
}
