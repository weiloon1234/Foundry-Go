package expressionqueries_test

import (
	"errors"
	"reflect"
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

func TestPostgresRowConditionalResults(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.UserFields()
		base := models.QueryUsers().OrderBy(u.Email.Asc())
		display := query.Coalesce(u.Nickname, u.Email)
		if got, err := query.SelectValue(base, display.Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{users[0].Email, "Bee", users[2].Email}) {
			t.Fatal("coalesce fallback", got, err)
		}
		if got, err := base.Where(display.Eq("Bee")).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("computed row predicate", err)
		}
		label := query.When(u.Age.Eq(20), u.Age.Param(2)).When(u.Age.Gt(20), u.Age.Param(10)).Else(u.Age.Param(0))
		// All result branches are parameters: PostgreSQL must compare numbers, not text.
		if got, err := query.SelectValue(models.QueryUsers(), label.Value()).Distinct().OrderBy(label.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{2, 10}) {
			t.Fatal("CASE parameters lost numeric type", got, err)
		}
		if got, err := base.Where(label.In()).Count(t.Context(), tx); err != nil || got != 0 {
			t.Fatal("empty computed IN", got, err)
		}
		if got, err := query.SelectValue(base.Where(label.In().Not(), u.Age.Eq(21)), u.Email.Param("kept").Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatal("empty IN disrupted surrounding bindings", got, err)
		}
		first := query.When(u.Age.Gte(20), u.Email.Param("first")).When(u.Age.Gte(21), u.Email.Param("later")).Else(u.Email.Param("none"))
		if got, err := query.SelectValue(base, first.Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"first", "first", "first"}) {
			t.Fatal("CASE branch order", got, err)
		}
		optional := query.When(u.Nickname.Like("B%"), u.Age).ElseNull()
		if got, err := query.SelectValue(base, optional.Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[int]{value.Null[int](), value.Of(21), value.Null[int]()}) {
			t.Fatal("CASE unknown condition/ELSE NULL", got, err)
		}
		removed := query.NullIf(display, u.Email.Param("Bee"))
		if got, err := query.SelectValue(base, removed.Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Of(users[0].Email), value.Null[string](), value.Of(users[2].Email)}) {
			t.Fatal("NULLIF semantics", got, err)
		}
		if got, err := base.Where(removed.IsNull()).RequireFirst(t.Context(), tx); err != nil || got.ID != users[1].ID {
			t.Fatal("nullable computed predicate", err)
		}
		nullable := query.NullIfNullable(u.Nickname, query.NullableRow(u.Email.Param("Bee")))
		if got, err := query.SelectValue(base, query.CoalesceNullable(nullable, query.NullFor(u.Email)).Value()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []value.Nullable[string]{value.Null[string](), value.Null[string](), value.Null[string]()}) {
			t.Fatal("nullable result layers", got, err)
		}
		if _, err := models.QueryUsers().Update(t.Context(), tx, users[0].ID, models.UserDraft{}.SetNickname("")); err != nil {
			return err
		}
		if got, err := query.SelectValue(base.Where(u.ID.Eq(users[0].ID)), display.Value()).RequireFirst(t.Context(), tx); err != nil || got != "" {
			t.Fatal("coalesce replaced an empty string", got, err)
		}
		return nil
	})
}

func TestPostgresSelectedConditionalValues(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		orders := models.QueryOrders()
		total := o.TotalCents.Sum()
		fallback := query.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())
		if got, err := query.SelectValue(orders.Where(o.TotalCents.Lt(0)), fallback).RequireFirst(t.Context(), tx); err != nil || got != decimal.FromInt64(0) {
			t.Fatal("empty aggregate fallback", got, err)
		}
		count := query.Count[models.Order]()
		selected := query.WhenValue(count.Gt(1), count.Value()).Else(count.Param(0).Value())
		if got, err := query.SelectValue(orders, selected).GroupBy(o.BuyerID.Group()).OrderBy(count.Desc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{2, 0}) {
			t.Fatal("CASE of grouped aggregates", got, err)
		}
		w := query.WindowFor(orders).OrderBy(o.TotalCents.Asc())
		number := query.RowNumber(w)
		windowValue := query.WhenValue(query.Grouped(o.TotalCents.Gt(1)), number).Else(count.Param(0).Value())
		if got, err := query.SelectValue(orders, windowValue).OrderBy(o.TotalCents.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{0, 2, 3}) {
			t.Fatal("conditional window result", got, err)
		}
		// A window may consume an ordinary conditional value over grouped counts.
		if got, err := query.SelectValue(orders, query.LagOr(selected, 1, int64(-1), query.WindowFor(orders).OrderBy(count.Desc()))).GroupBy(o.BuyerID.Group()).OrderBy(count.Desc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{-1, 2}) {
			t.Fatal("conditional aggregate did not establish window input grouping", got, err)
		}
		filtered := query.Count[models.Order]().Filter(query.When(o.TotalCents.Gt(1), o.TotalCents.Param(1)).Else(o.TotalCents.Param(0)).Eq(1))
		if got, err := query.SelectValue(orders, filtered.Value()).RequireFirst(t.Context(), tx); err != nil || got != 2 {
			t.Fatal("computed aggregate filter", got, err)
		}
		return nil
	})
}

type selectedAlias struct{}
type orderAlias struct{}

func TestPostgresConditionalCompositionAndFailures(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.UserFields(), models.OrderFields()
		people := models.QueryUsers()
		common := query.As[selectedAlias](query.CTE("eligible", people.Where(u.Age.Gt(20))), "selected")
		f := models.UserFieldsAt(common.Scope())
		label := query.When(u.ID.InQuery(query.SelectValue(common, f.ID.Value())), u.Email.Param("yes")).Else(u.Email.Param("no"))
		if got, err := query.SelectValue(people, label.Value()).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"no", "yes", "yes"}) {
			t.Fatal("conditional CTE discovery", got, err)
		}
		// A selected scalar owns its grouping and discovers dependencies even
		// when the only reference to its CTE is inside a conditional value.
		chosen := query.SelectValue(common, f.Age.Value()).OrderBy(f.Age.Desc()).Limit(1)
		age := query.CoalesceValue(query.ScalarQuery(people, chosen), u.Age.Param(-1).Value())
		if got, err := query.SelectValue(people, age).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int{22, 22, 22}) {
			t.Fatal("conditional scalar/CTE composition", got, err)
		}
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		link := query.Correlate(people, orders)
		parent := models.UserFieldsAt(query.OuterScope(link, people.Scope()))
		child := models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		bought := link.Where(child.BuyerID.EqColumn(parent.ID)).Exists()
		withOrders := query.When(bought, u.Email.Param("yes")).Else(u.Email.Param("no"))
		if got, err := query.SelectValue(people, withOrders.Value()).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []string{"yes", "yes", "no"}) {
			t.Fatal("conditional correlation", got, err)
		}
		display := query.Coalesce(u.Nickname, u.Email)
		projection := reports.SelectUserSummary(people, reports.UserSummarySelection[models.User]{ID: u.ID.Value(), Email: display.Value(), Nickname: u.Nickname.Value(), Status: u.Status.Value()})
		alias := query.As[selectedAlias](projection, "displayed")
		fields := reports.UserSummaryFieldsAt(alias.Scope())
		if got, err := query.SelectValue(alias, fields.Email.Value()).Where(fields.Email.Eq("Bee")).RequireFirst(t.Context(), tx); err != nil || got != "Bee" {
			t.Fatal("typed projected conditional output", got, err)
		}
		r := models.UserRelations()
		computation := query.Count[models.Order]().Filter(query.When(o.TotalCents.Gt(1), o.TotalCents.Param(1)).Else(o.TotalCents.Param(0)).Eq(1))
		slot := models.UserAggregates().OrderCount.Using(query.Related(r.Orders, computation))
		rows, err := people.With(slot).OrderBy(u.Email.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for i, row := range rows {
			count, loaded := row.OrderCount.Get()
			if !loaded || count != []int64{1, 1, 0}[i] {
				t.Fatal("conditional relation aggregate", row)
			}
		}
		// Even unreachable parameters must pass their codec before any I/O.
		invalid := query.When(u.Age.Gt(0), u.Status).Else(u.Status.Param(models.Status("invalid")))
		if got, err := query.SelectValue(people, invalid.Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) || got != nil {
			t.Fatal("invalid parameter reached execution", err)
		}
		// The same computed row predicates also constrain model writes.
		updated, err := people.Where(display.Eq("Bee")).Update(t.Context(), tx, users[1].ID, models.UserDraft{}.SetNickname("B updated"))
		if err != nil || updated.Nickname != value.Of("B updated") {
			t.Fatal("computed update predicate", updated, err)
		}
		return nil
	})
}
