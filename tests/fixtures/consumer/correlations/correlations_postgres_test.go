package correlations_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type otherOrderAlias struct{}

func TestPostgresTypedCorrelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error { return checkCorrelatedQueries(t, tx, users) })
}

func checkCorrelatedQueries(t *testing.T, tx *database.Tx, users []models.User) error {
	u := models.Users
	link := rowset.Correlate(rowset.From(models.Users.Query()), rowset.From(models.Orders.Query()))
	buyer := rowset.OuterScope(link, rowset.ModelScope(models.Users.Query()))
	order := rowset.InnerScope(link, rowset.ModelScope(models.Orders.Query()))
	matched := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
	counts := rowset.SelectCorrelatedScalar(matched, expr.CountFieldAt(order, models.Orders.ID).Value())
	totals := rowset.SelectCorrelatedScalar(matched, expr.SumAt(order, models.Orders.TotalCents).Value())
	r := reports.UserOrderStatsRows
	report := rowset.OrderBy(rowset.Select(rowset.From(models.Users.Query()), r.Row,
		rowset.Map(r.ID, expr.Value(u.ID)), rowset.Map(r.Email, expr.Value(u.Email)),
		rowset.MapSelected(r.Orders, rowset.CorrelatedScalarRowQuery(counts).Value()),
		rowset.MapSelected(r.Total, rowset.CorrelatedScalarNullableRowQuery(totals).Value())), expr.Value(u.Email).Asc())
	rows, err := rowset.Read(report).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 {
		t.Fatal("correlated aggregates changed outer row cardinality")
	}
	for i, want := range []int64{2, 1, 0} {
		if n, ok := rows[i].Orders.Get(); !ok || n != want {
			t.Error("per-user correlated count changed", i, n, want)
		}
	}
	if sum, ok := rows[0].Total.Get(); !ok || sum.String() != "3" || !rows[2].Total.IsNull() {
		t.Error("correlated exact sums lost NULL or precision")
	}
	ids := rowset.SelectCorrelatedScalar(matched, expr.Row(expr.At(order, models.Orders.BuyerID)).Value())
	for _, predicate := range []expr.Predicate[models.User]{matched.Exists(), ids.Exists(), rowset.InCorrelatedQuery(expr.Value(u.ID), ids), rowset.InNullableCorrelatedQuery(expr.Value(u.ID), rowset.SelectCorrelatedScalar(matched, expr.Nullable(expr.Row(expr.At(order, models.Orders.BuyerID)).Value())))} {
		if n, err := expr.Where(models.Users.Query(), predicate).Count(t.Context(), tx); err != nil || n != 2 {
			t.Error("correlated EXISTS/IN mismatch", n, err)
		}
	}
	if n, err := expr.Where(models.Users.Query(), expr.Not(matched.Exists())).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("correlated NOT EXISTS mismatch", err)
	}
	// Only the first buyer has an order with a larger sibling order. The nested
	// correlation refers to both its parent order and its grandparent user.
	other := rowset.As[otherOrderAlias](models.Orders.Query(), "other_order")
	nested := rowset.Correlate(link, other)
	grandparent := rowset.OuterScope(nested, buyer)
	parent := rowset.OuterScope(nested, order)
	child := rowset.InnerScope(nested, rowset.Scope(other))
	larger := nested.Where(expr.Equal(expr.At(child, models.Orders.BuyerID), expr.At(grandparent, models.Users.ID)), expr.Greater(expr.At(child, models.Orders.TotalCents), expr.At(parent, models.Orders.TotalCents)))
	found, err := expr.Where(models.Users.Query(), matched.Where(larger.Exists()).Exists()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(found) != 1 || found[0].ID != users[0].ID {
		t.Error("nested correlation lost parent/grandparent references")
	}
	// Non-equality column predicates retain order/value types and SQL operators.
	for _, test := range []struct {
		predicate expr.Predicate[rowset.Correlation[models.User, models.Order]]
		want      int64
	}{
		{expr.Less(expr.At(order, models.Orders.TotalCents), expr.At(order, models.Orders.TotalCents)), 0},
		{expr.LessOrEqual(expr.At(order, models.Orders.TotalCents), expr.At(order, models.Orders.TotalCents)), 2},
		{expr.Greater(expr.At(order, models.Orders.TotalCents), expr.At(order, models.Orders.TotalCents)), 0},
		{expr.GreaterOrEqual(expr.At(order, models.Orders.TotalCents), expr.At(order, models.Orders.TotalCents)), 2},
		{expr.NotEqual(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)), 0},
	} {
		if n, err := expr.Where(models.Users.Query(), matched.Where(test.predicate).Exists()).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("column comparison mismatch", n, test.want, err)
		}
	}
	// Outer grouping validates references in SELECT/HAVING; WHERE remains before
	// grouping. A key group can safely contain a correlated scalar count.
	if groups, err := rowset.Read(rowset.GroupBy(report, expr.Group(expr.Value(u.ID)), expr.Group(expr.Value(u.Email)))).All(t.Context(), tx); err != nil || len(groups) != 3 {
		t.Error("grouped correlated report failed", err)
	}
	grouped := rowset.Having(rowset.GroupBy(rowset.FromModel(models.Users.Query()), expr.Group(expr.Value(u.ID))), expr.Grouped(matched.Exists()))
	if groups, err := rowset.ReadScalar(rowset.SelectScalar(rowset.FromRows(grouped), expr.Count[models.User]().Value())).All(t.Context(), tx); err != nil || len(groups) != 2 {
		t.Error("correlated HAVING failed", err)
	}
	ungrouped := rowset.Having(rowset.FromModel(models.Users.Query()), expr.Grouped(matched.Exists()))
	if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.FromRows(ungrouped), expr.Count[models.User]().Value()))); !errors.Is(err, fault.Invalid) {
		t.Error("ungrouped outer reference reached SQL", err)
	}
	if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.CorrelatedScalarRowQuery(rowset.SelectCorrelatedScalar(link, expr.CountFieldAt(buyer, models.Users.ID).Value())).Value()))); !errors.Is(err, fault.Invalid) {
		t.Error("outer-only aggregate changed SELECT ownership", err)
	}
	// A limited scalar is per outer row. A filtered/limited aliased source retains
	// its global window before correlation, just as it does before a join.
	chosen := rowset.CorrelatedScalarRowQuery(rowset.SelectCorrelatedScalar(matched, expr.Row(expr.At(order, models.Orders.ID)).Value()).OrderBy(expr.At(order, models.Orders.TotalCents).Desc()).Limit(1)).Value()
	if selected, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), chosen)).OrderBy(expr.Value(u.Email).Asc()).All(t.Context(), tx); err != nil || len(selected) != 3 || selected[0].IsNull() || !selected[2].IsNull() {
		t.Error("correlated scalar window mismatch", err)
	}
	limited := rowset.As[otherOrderAlias](models.Orders.Query().OrderBy(models.Orders.TotalCents.Asc()).Limit(1), "first_order")
	windowed := rowset.Correlate(rowset.From(models.Users.Query()), limited)
	wb := rowset.OuterScope(windowed, rowset.ModelScope(models.Users.Query()))
	wo := rowset.InnerScope(windowed, rowset.Scope(limited))
	if n, err := expr.Where(models.Users.Query(), windowed.Where(expr.Equal(expr.At(wo, models.Orders.BuyerID), expr.At(wb, models.Users.ID))).Exists()).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("source window moved past correlation", err)
	}
	err = tx.Savepoint(t.Context(), func(child *database.Tx) error {
		rows, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.CorrelatedScalarRowQuery(ids).Value())).All(t.Context(), child)
		var detail *database.Error
		if rows != nil || !errors.As(err, &detail) || detail.SQLState() != "21000" {
			t.Error("correlated scalar cardinality failure changed", err)
		}
		return err
	})
	if err == nil {
		t.Error("multi-row correlated scalar unexpectedly succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := rowset.Read(report).All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled correlated report reached database", err)
	}
	// Correlated predicates participate in ordinary scoped writes.
	if updated, err := orm.Update(t.Context(), tx, expr.Where(models.Users.Query(), matched.Exists()), models.Users.Key(), users[0].ID, models.Users.Age.Set(30)); err != nil || updated.Age != 30 {
		t.Error("correlated model update failed", err)
	}
	if _, err := orm.Update(t.Context(), tx, expr.Where(models.Users.Query(), matched.Exists()), models.Users.Key(), users[2].ID, models.Users.Age.Set(30)); !errors.Is(err, database.NotFound) {
		t.Error("correlated write failed to constrain its key", err)
	}
	// A through relation assigns framework aliases to the outer model. Its
	// correlation must follow that alias while preserving its inner table names.
	if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
		return err
	}
	for _, friend := range users[:2] {
		if _, err := models.Friendships.Insert(t.Context(), tx, models.Friendships.FromID.Set(users[2].ID), models.Friendships.ToID.Set(friend.ID), models.Friendships.Note.Set("query test")); err != nil {
			return err
		}
	}
	loaded, err := models.Users.Query().Where(u.ID.Eq(users[2].ID)).With(models.UserLinks.Friends.Where(expr.RelationFilter(models.UserLinks.Friends, matched.Where(larger.Exists()).Exists()))).Require(t.Context(), tx)
	if err != nil {
		return err
	}
	friends, ok := loaded.Friends.Get()
	if !ok || len(friends) != 1 || friends[0].Model.ID != users[0].ID {
		t.Error("relation qualification changed nested correlation")
	}
	return nil
}
