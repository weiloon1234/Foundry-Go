package ctes_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type eligibleAlias struct{}
type otherAlias struct{}
type totalsAlias struct{}

func TestPostgresTypedCTEs(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		if err := checkSharedCTEs(t, tx, users); err != nil {
			return err
		}
		if err := checkProjectedCTEs(t, tx, users); err != nil {
			return err
		}
		return checkCTEModelFilters(t, tx, users)
	})
}

func checkSharedCTEs(t *testing.T, tx *database.Tx, users []models.User) error {
	u := models.UserFields()
	eligible := query.CTE("eligible_users", models.QueryUsers().Where(u.Age.Gte(21))).Materialized()
	first, second := query.As[eligibleAlias](eligible, "first_user"), query.As[otherAlias](eligible, "second_user")
	a, b := models.UserFieldsAt(first.Scope()), models.UserFieldsAt(second.Scope())
	joined := query.InnerJoin(first, second, query.On(a.ID, b.ID))
	fields := models.UserFieldsAt(query.LeftScope(joined, first.Scope()))
	names := query.SelectValue(joined, fields.Email.Value()).OrderBy(fields.Email.Asc())
	s, err := names.Compile()
	if err != nil {
		return err
	}
	if strings.Count(s.SQL(), "WITH ") != 1 || strings.Count(s.SQL(), `FROM "users"`) != 1 || !strings.Contains(s.SQL(), "AS MATERIALIZED (") {
		t.Fatal("shared CTE definition was duplicated", s.SQL())
	}
	rows, err := names.All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 2 || rows[0] != users[1].Email || rows[1] != users[2].Email {
		t.Fatal("shared typed CTE join returned wrong rows")
	}
	// A dependent CTE retains the complete record and orders dependencies first.
	dependent := query.CTE("dependent_users", eligible)
	d := query.As[eligibleAlias](dependent, "dependent")
	df := models.UserFieldsAt(d.Scope())
	statuses := query.SelectValue(d, df.Status.Value()).Where(df.Status.Eq(models.StatusActive))
	if n, err := statuses.Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("dependent CTE lost enum codec or input", err)
	}
	if exists, err := statuses.Exists(t.Context(), tx); err != nil || !exists {
		t.Error("CTE existence failed", err)
	}
	stop := errors.New("stop CTE stream")
	if err := names.Each(t.Context(), tx, func(string) error { return stop }); !errors.Is(err, stop) {
		t.Error("CTE stream lost callback error", err)
	}
	if first, err := names.RequireFirst(t.Context(), tx); err != nil || first != users[1].Email {
		t.Error("CTE stream did not release rows", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := names.All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled CTE reached executor", err)
	}
	return nil
}

func checkProjectedCTEs(t *testing.T, tx *database.Tx, users []models.User) error {
	o := models.OrderFields()
	totals := reports.ProjectBuyerTotals(models.QueryOrders()).SelectBuyerID(o.BuyerID.Value()).
		SelectTotal(o.TotalCents.Sum().Value()).SelectOrders(query.Count[models.Order]().Value()).
		SelectAverage(o.TotalCents.Avg().Value()).Query().GroupBy(o.BuyerID.Group()).
		Having(query.Count[models.Order]().Gte(2)).OrderBy(query.Count[models.Order]().Desc()).Limit(1)
	definition := query.CTE("buyer_totals", totals).NotMaterialized()
	source := query.As[totalsAlias](definition, "totals")
	f := reports.BuyerTotalsFieldsAt(source.Scope())
	buyers := query.SelectValue(source, f.BuyerID.Value()).Where(f.Total.Gt(decimal.FromInt64(2)))
	selected, err := models.QueryUsers().Where(models.UserFields().ID.InQuery(buyers)).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Fatal("projected CTE lost GROUP BY/HAVING/window or decimal predicates")
	}
	// A CTE record keeps declared aliases and codecs through outer joins.
	u := query.As[eligibleAlias](models.QueryUsers(), "buyer")
	joined := query.LeftJoin(u, source, query.On(models.UserFieldsAt(u.Scope()).ID, f.BuyerID))
	user := models.UserFieldsAt(query.LeftScope(joined, u.Scope()))
	stats := reports.BuyerTotalsNullableFieldsAt(query.NullableRightScope(joined, source.Scope()))
	report := reports.ProjectUserOrderStats(joined).SelectID(user.ID.Value()).SelectEmail(user.Email.Value()).
		SelectTotal(stats.Total.Value()).SelectOrders(stats.Orders.Value()).Query().OrderBy(user.Email.Asc())
	rows, err := report.All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 {
		t.Fatal("CTE outer join lost unmatched users")
	}
	if n, ok := rows[0].Orders.Get(); !ok || n != 2 {
		t.Error("CTE count lost its value")
	}
	if total, ok := rows[0].Total.Get(); !ok || total.String() != "3" {
		t.Error("CTE decimal result changed")
	}
	if !rows[1].Orders.IsNull() || !rows[2].Total.IsNull() {
		t.Error("CTE outer join lost nullability")
	}
	// Hoisted definitions may contain their own explicit row correlations.
	filtered := query.CTE("buyers_with_orders", models.QueryUsers().WhereHas(models.UserRelations().Orders.Where(o.TotalCents.Gt(2))))
	a := query.As[otherAlias](filtered, "filtered")
	if ids, err := query.SelectValue(a, models.UserFieldsAt(a.Scope()).ID.Value()).All(t.Context(), tx); err != nil || len(ids) != 1 || ids[0] != users[1].ID {
		t.Error("CTE lost its internal relationship correlation", err)
	}
	return nil
}

func checkCTEModelFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.UserFields(), models.OrderFields()
	eligible := query.CTE("eligible_users", models.QueryUsers().Where(u.Age.Gte(21)))
	a := query.As[eligibleAlias](eligible, "eligible")
	f := models.UserFieldsAt(a.Scope())
	ids := query.SelectValue(a, f.ID.Value())
	// CTE references can live in an explicitly correlated inner query.
	correlation := query.Correlate(models.QueryOrders(), a)
	inner := models.UserFieldsAt(query.InnerScope(correlation, a.Scope()))
	outer := models.OrderFieldsAt(query.OuterScope(correlation, models.QueryOrders().Scope()))
	match := correlation.Where(inner.ID.EqColumn(outer.BuyerID)).Exists()
	if n, err := models.QueryOrders().Where(match).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("CTE correlation returned wrong orders", err)
	}
	// Automatic relation aliases must not capture names inside CTE definitions.
	referrals := models.UserRelations().Referrals.Where(u.ID.InQuery(ids))
	loaded, err := models.QueryUsers().WhereHas(referrals).With(referrals).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(loaded) != 1 || loaded[0].ID != users[0].ID {
		t.Fatal("CTE relationship filter mismatch")
	}
	if children, ok := loaded[0].Referrals.Get(); !ok || len(children) != 1 || children[0].ID != users[1].ID {
		t.Error("CTE eager filter mismatch")
	}
	if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
		return err
	}
	if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[0].ID).SetToID(users[1].ID).SetNote("active")); err != nil {
		return err
	}
	friends := models.UserRelations().Friends.Where(u.ID.InQuery(ids))
	if rows, err := models.QueryUsers().WhereHas(friends).With(friends).All(t.Context(), tx); err != nil || len(rows) != 1 {
		t.Error("CTE through filter failed", err)
	}
	count := models.UserAggregates().OrderCount.Using(query.Related(models.UserRelations().Orders.Where(o.BuyerID.InQuery(ids)), query.Count[models.Order]()))
	counted, err := models.QueryUsers().With(count).OrderBy(u.Email.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(counted) != 3 {
		t.Fatal("CTE aggregate lost parent rows")
	}
	for i, want := range []int64{0, 1, 0} {
		if n, ok := counted[i].OrderCount.Get(); !ok || n != want {
			t.Error("CTE aggregate filter mismatch")
		}
	}
	// CTE-bound predicates share the mutation's parameter numbering and transaction.
	builder := models.QueryUsers().Where(u.ID.InQuery(ids))
	if updated, err := builder.Update(t.Context(), tx, users[1].ID, models.UserDraft{}.SetAge(25)); err != nil || updated.Age != 25 {
		t.Error("CTE-scoped update failed", err)
	}
	if _, err := builder.Update(t.Context(), tx, users[0].ID, models.UserDraft{}.SetAge(25)); !errors.Is(err, database.NotFound) {
		t.Error("CTE filter did not constrain update", err)
	}
	if _, err := builder.Delete(t.Context(), tx, users[2].ID); err != nil {
		return err
	}
	if n, err := builder.Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("CTE-scoped delete failed", err)
	}
	return nil
}

func TestInvalidCTEsNeverExecute(t *testing.T) {
	for _, definition := range []query.CommonTable[models.User]{
		{}, query.CTE[models.User]("empty", nil), query.CTE("users", models.QueryUsers()),
		query.CTE("bad", models.QueryUsers().Where(models.UserFields().Status.Eq(models.Status("invalid")))),
	} {
		source := query.As[eligibleAlias](definition, "eligible")
		ids := query.SelectValue(source, models.UserFieldsAt(source.Scope()).ID.Value())
		if rows, err := ids.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid CTE reached executor", err)
		}
	}
	first := query.CTE("same_name", models.QueryUsers())
	second := query.CTE("same_name", models.QueryUsers().Where(models.UserFields().Age.Gt(18)))
	a, b := query.As[eligibleAlias](first, "a"), query.As[otherAlias](second, "b")
	joined := query.InnerJoin(a, b, query.On(models.UserFieldsAt(a.Scope()).ID, models.UserFieldsAt(b.Scope()).ID))
	ids := query.SelectValue(joined, models.UserFieldsAt(query.LeftScope(joined, a.Scope())).ID.Value())
	if rows, err := ids.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("conflicting CTE definitions reached executor", err)
	}
	// Pin the public result type: CTE IDs remain IDs of the original model.
	var _ query.ValueQuerySource[model.ID[models.User]] = ids
}
