package correlations_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type otherOrderAlias struct{}

func TestPostgresTypedCorrelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error { return checkCorrelatedQueries(t, tx, users) })
}

func checkCorrelatedQueries(t *testing.T, tx *database.Tx, users []models.User) error {
	u := models.UserFields()
	link := query.Correlate(models.QueryUsers(), models.QueryOrders())
	buyer := models.UserFieldsAt(query.OuterScope(link, models.QueryUsers().Scope()))
	order := models.OrderFieldsAt(query.InnerScope(link, models.QueryOrders().Scope()))
	matched := link.Where(order.BuyerID.EqColumn(buyer.ID))
	counts := query.SelectCorrelatedValue(matched, order.ID.Count().Value())
	totals := query.SelectCorrelatedValue(matched, order.TotalCents.Sum().Value())
	report := reports.ProjectUserOrderStats(models.QueryUsers()).SelectID(u.ID.Value()).SelectEmail(u.Email.Value()).
		SelectOrders(query.CorrelatedScalarQuery(counts)).SelectTotal(query.CorrelatedScalarNullableQuery(totals)).Query().OrderBy(u.Email.Asc())
	rows, err := report.All(t.Context(), tx)
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
	ids := query.SelectCorrelatedValue(matched, order.BuyerID.Value())
	for _, predicate := range []query.Predicate[models.User]{matched.Exists(), ids.Exists(), u.ID.InCorrelatedQuery(ids), u.ID.InNullableCorrelatedQuery(query.SelectCorrelatedValue(matched, query.Nullable(order.BuyerID.Value())))} {
		if n, err := models.QueryUsers().Where(predicate).Count(t.Context(), tx); err != nil || n != 2 {
			t.Error("correlated EXISTS/IN mismatch", n, err)
		}
	}
	if n, err := models.QueryUsers().Where(matched.Exists().Not()).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("correlated NOT EXISTS mismatch", err)
	}
	// Only the first buyer has an order with a larger sibling order. The nested
	// correlation refers to both its parent order and its grandparent user.
	other := query.As[otherOrderAlias](models.QueryOrders(), "other_order")
	nested := query.Correlate(link, other)
	grandparent := models.UserFieldsAt(query.OuterScope(nested, query.OuterScope(link, models.QueryUsers().Scope())))
	parent := models.OrderFieldsAt(query.OuterScope(nested, query.InnerScope(link, models.QueryOrders().Scope())))
	child := models.OrderFieldsAt(query.InnerScope(nested, other.Scope()))
	larger := nested.Where(child.BuyerID.EqColumn(grandparent.ID), child.TotalCents.GtColumn(parent.TotalCents))
	found, err := models.QueryUsers().Where(matched.Where(larger.Exists()).Exists()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(found) != 1 || found[0].ID != users[0].ID {
		t.Error("nested correlation lost parent/grandparent references")
	}
	// Non-equality column predicates retain order/value types and SQL operators.
	for _, test := range []struct {
		predicate query.Predicate[query.Correlation[models.User, models.Order]]
		want      int64
	}{
		{order.TotalCents.LtColumn(order.TotalCents), 0},
		{order.TotalCents.LteColumn(order.TotalCents), 2},
		{order.TotalCents.GtColumn(order.TotalCents), 0},
		{order.TotalCents.GteColumn(order.TotalCents), 2},
		{order.BuyerID.NeColumn(buyer.ID), 0},
	} {
		if n, err := models.QueryUsers().Where(matched.Where(test.predicate).Exists()).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("column comparison mismatch", n, test.want, err)
		}
	}
	// Outer grouping validates references in SELECT/HAVING; WHERE remains before
	// grouping. A key group can safely contain a correlated scalar count.
	if groups, err := report.GroupBy(u.ID.Group(), u.Email.Group()).All(t.Context(), tx); err != nil || len(groups) != 3 {
		t.Error("grouped correlated report failed", err)
	}
	if groups, err := query.SelectValue(models.QueryUsers(), query.Count[models.User]().Value()).GroupBy(u.ID.Group()).Having(query.Grouped(matched.Exists())).All(t.Context(), tx); err != nil || len(groups) != 2 {
		t.Error("correlated HAVING failed", err)
	}
	if _, err := query.SelectValue(models.QueryUsers(), query.Count[models.User]().Value()).Having(query.Grouped(matched.Exists())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Error("ungrouped outer reference reached SQL", err)
	}
	if _, err := query.SelectValue(models.QueryUsers(), query.CorrelatedScalarQuery(query.SelectCorrelatedValue(link, buyer.ID.Count().Value()))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Error("outer-only aggregate changed SELECT ownership", err)
	}
	// A limited scalar is per outer row. A filtered/limited aliased source retains
	// its global window before correlation, just as it does before a join.
	chosen := query.CorrelatedScalarQuery(query.SelectCorrelatedValue(matched, order.ID.Value()).OrderBy(order.TotalCents.Desc()).Limit(1))
	if selected, err := query.SelectValue(models.QueryUsers(), chosen).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || len(selected) != 3 || selected[0].IsNull() || !selected[2].IsNull() {
		t.Error("correlated scalar window mismatch", err)
	}
	limited := query.As[otherOrderAlias](models.QueryOrders().OrderBy(models.OrderFields().TotalCents.Asc()).Limit(1), "first_order")
	windowed := query.Correlate(models.QueryUsers(), limited)
	wb := models.UserFieldsAt(query.OuterScope(windowed, models.QueryUsers().Scope()))
	wo := models.OrderFieldsAt(query.InnerScope(windowed, limited.Scope()))
	if n, err := models.QueryUsers().Where(windowed.Where(wo.BuyerID.EqColumn(wb.ID)).Exists()).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("source window moved past correlation", err)
	}
	err = tx.Savepoint(t.Context(), func(child *database.Tx) error {
		rows, err := query.SelectValue(models.QueryUsers(), query.CorrelatedScalarQuery(ids)).All(t.Context(), child)
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
	if rows, err := report.All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled correlated report reached database", err)
	}
	// Correlated predicates participate in ordinary scoped writes.
	if updated, err := models.QueryUsers().Where(matched.Exists()).Update(t.Context(), tx, users[0].ID, models.UserDraft{}.SetAge(30)); err != nil || updated.Age != 30 {
		t.Error("correlated model update failed", err)
	}
	if _, err := models.QueryUsers().Where(matched.Exists()).Update(t.Context(), tx, users[2].ID, models.UserDraft{}.SetAge(30)); !errors.Is(err, database.NotFound) {
		t.Error("correlated write failed to constrain its key", err)
	}
	// A through relation assigns framework aliases to the outer model. Its
	// correlation must follow that alias while preserving its inner table names.
	if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
		return err
	}
	for _, friend := range users[:2] {
		if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[2].ID).SetToID(friend.ID).SetNote("query test")); err != nil {
			return err
		}
	}
	loaded, err := models.QueryUsers().Where(u.ID.Eq(users[2].ID)).With(models.UserRelations().Friends.Where(matched.Where(larger.Exists()).Exists())).RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	friends, ok := loaded.Friends.Get()
	if !ok || len(friends) != 1 || friends[0].Model.ID != users[0].ID {
		t.Error("relation qualification changed nested correlation")
	}
	return nil
}
