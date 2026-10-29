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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
	u := models.Users
	eligible := rowset.CTE("eligible_users", rowset.ModelRecord(models.Users.Query().Where(orm.NumberGte(u.Age, 21)))).Materialized()
	first, second := rowset.AsRecord[eligibleAlias](eligible, "first_user"), rowset.AsRecord[otherAlias](eligible, "second_user")
	a, b := rowset.Scope(first), rowset.Scope(second)
	joined := rowset.InnerJoin(first, second, rowset.On(expr.At(a, orm.KeyExpr(models.Users.ID)), expr.At(b, orm.KeyExpr(models.Users.ID))))
	fields := rowset.LeftScope(joined, rowset.Scope(first))
	names := rowset.OrderScalar(rowset.SelectScalar(joined, expr.Row(expr.At(fields, orm.TextExpr(models.Users.Email))).Value()), expr.At(fields, orm.TextExpr(models.Users.Email)).Asc())
	s, err := rowset.CompileRead(rowset.ReadScalar(names))
	if err != nil {
		return err
	}
	if strings.Count(s.SQL(), "WITH ") != 1 || strings.Count(s.SQL(), `FROM "users"`) != 1 || !strings.Contains(s.SQL(), "AS MATERIALIZED (") {
		t.Fatal("shared CTE definition was duplicated", s.SQL())
	}
	rows, err := rowset.ReadScalar(names).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 2 || rows[0] != users[1].Email || rows[1] != users[2].Email {
		t.Fatal("shared typed CTE join returned wrong rows")
	}
	// A dependent CTE retains the complete record and orders dependencies first.
	dependent := rowset.CTE("dependent_users", eligible)
	d := rowset.AsRecord[eligibleAlias](dependent, "dependent")
	df := rowset.Scope(d)
	statuses := rowset.WhereScalar(rowset.SelectScalar(d, expr.Row(expr.At(df, orm.EnumExpr(models.Users.Status))).Value()), expr.At(df, orm.EnumExpr(models.Users.Status)).Eq(models.StatusActive))
	if n, err := rowset.ReadScalar(statuses).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("dependent CTE lost enum codec or input", err)
	}
	if exists, err := rowset.ReadScalar(statuses).Exists(t.Context(), tx); err != nil || !exists {
		t.Error("CTE existence failed", err)
	}
	stop := errors.New("stop CTE stream")
	if err := rowset.EachRead(t.Context(), tx, rowset.ReadScalar(names), func(string) error { return stop }); !errors.Is(err, stop) {
		t.Error("CTE stream lost callback error", err)
	}
	if first, err := rowset.ReadScalar(names).RequireFirst(t.Context(), tx); err != nil || first != users[1].Email {
		t.Error("CTE stream did not release rows", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := rowset.ReadScalar(names).All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled CTE reached executor", err)
	}
	return nil
}

func checkProjectedCTEs(t *testing.T, tx *database.Tx, users []models.User) error {
	o := models.Orders
	totals := rowset.Limit(rowset.OrderBy(rowset.Having(rowset.GroupBy(rowset.Select(rowset.From(models.Orders.Query()), reports.BuyerTotalRows.Row,
		rowset.Map(reports.BuyerTotalRows.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))),
		rowset.MapSelected(reports.BuyerTotalRows.Total, expr.Sum(orm.NumberExpr(o.TotalCents)).Value()),
		rowset.MapSelected(reports.BuyerTotalRows.Orders, expr.Count[models.Order]().Value()),
		rowset.MapSelected(reports.BuyerTotalRows.Average, expr.Avg(orm.NumberExpr(o.TotalCents)).Value()),
	), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.Count[models.Order]().Gte(2)), expr.Count[models.Order]().Desc()), 1)
	definition := rowset.CTE("buyer_totals", rowset.RecordOf(totals)).NotMaterialized()
	source := rowset.AsRecord[totalsAlias](definition, "totals")
	f := rowset.Scope(source)
	buyers := rowset.WhereScalar(rowset.SelectScalar(source, expr.Row(rowset.At(f, reports.BuyerTotalRows.BuyerID)).Value()), expr.OrderNullableRow(rowset.At(f, reports.BuyerTotalRows.Total)).Gt(decimal.FromInt64(2)))
	selected, err := orm.All(t.Context(), tx, expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(orm.KeyExpr(models.Users.ID)), buyers)))
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Fatal("projected CTE lost GROUP BY/HAVING/window or decimal predicates")
	}
	// A CTE record keeps declared aliases and codecs through outer joins.
	u := rowset.As[eligibleAlias](models.Users.Query(), "buyer")
	joined := rowset.LeftJoin(u, source, rowset.OnEqual(expr.At(rowset.Scope(u), orm.KeyExpr(models.Users.ID)), rowset.At(f, reports.BuyerTotalRows.BuyerID)))
	user := rowset.LeftScope(joined, rowset.Scope(u))
	stats := rowset.NullableRightScope(joined, rowset.Scope(source))
	report := rowset.OrderBy(rowset.Select(joined, reports.UserOrderStatsRows.Row,
		rowset.Map(reports.UserOrderStatsRows.ID, expr.At(user, orm.KeyExpr(models.Users.ID))),
		rowset.Map(reports.UserOrderStatsRows.Email, expr.At(user, orm.TextExpr(models.Users.Email))),
		rowset.MapSelected(reports.UserOrderStatsRows.Total, expr.Row(rowset.AtNullableValue(stats, reports.BuyerTotalRows.Total)).Value()),
		rowset.MapSelected(reports.UserOrderStatsRows.Orders, expr.Row(rowset.AtNullable(stats, reports.BuyerTotalRows.Orders)).Value()),
	), expr.At(user, orm.TextExpr(models.Users.Email)).Asc())
	rows, err := rowset.Read(report).All(t.Context(), tx)
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
	filtered := rowset.CTE("buyers_with_orders", rowset.ModelRecord(models.Users.Query().Where(orm.Has(models.UserLinks.Orders.Where(orm.NumberGt(o.TotalCents, 2))))))
	a := rowset.AsRecord[otherAlias](filtered, "filtered")
	if ids, err := rowset.ReadScalar(rowset.SelectScalar(a, expr.Row(expr.At(rowset.Scope(a), orm.KeyExpr(models.Users.ID))).Value())).All(t.Context(), tx); err != nil || len(ids) != 1 || ids[0] != users[1].ID {
		t.Error("CTE lost its internal relationship correlation", err)
	}
	return nil
}

func checkCTEModelFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.Users, models.Orders
	eligible := rowset.CTE("eligible_users", rowset.ModelRecord(models.Users.Query().Where(orm.NumberGte(u.Age, 21))))
	a := rowset.AsRecord[eligibleAlias](eligible, "eligible")
	f := rowset.Scope(a)
	ids := rowset.SelectScalar(a, expr.Row(expr.At(f, orm.KeyExpr(models.Users.ID))).Value())
	// CTE references can live in an explicitly correlated inner expr.

	correlation := rowset.Correlate(rowset.From(models.Orders.Query()), a)
	inner := rowset.InnerScope(correlation, rowset.Scope(a))
	outer := rowset.OuterScope(correlation, rowset.ModelScope(models.Orders.Query()))
	match := correlation.Where(expr.Equal(expr.At(inner, orm.KeyExpr(models.Users.ID)), expr.At(outer, orm.ColumnExpr(models.Orders.BuyerID)))).Exists()
	if n, err := orm.Count(t.Context(), tx, expr.Where(models.Orders.Query(), match)); err != nil || n != 1 {
		t.Error("CTE correlation returned wrong orders", err)
	}
	// Automatic relation aliases must not capture names inside CTE definitions.
	referrals := models.UserLinks.Referrals.Where(expr.RelationFilter(models.UserLinks.Referrals, rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids)))
	loaded, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.Has(referrals)).With(referrals))
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
	if _, err := orm.Insert(t.Context(), tx, models.Friendships.Table, orm.ColumnSet(models.Friendships.FromID, users[0].ID), orm.ColumnSet(models.Friendships.ToID, users[1].ID), orm.TextSet(models.Friendships.Note, "active")); err != nil {
		return err
	}
	friends := models.UserLinks.Friends.Where(expr.RelationFilter(models.UserLinks.Friends, rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids)))
	if rows, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.Has(friends)).With(friends)); err != nil || len(rows) != 1 {
		t.Error("CTE through filter failed", err)
	}
	count := models.Users.OrderCount.Using(expr.Related(models.UserLinks.Orders.Where(expr.RelationFilter(models.UserLinks.Orders, rowset.InQuery(expr.Value(orm.ColumnExpr(o.BuyerID)), ids))), expr.Count[models.Order]()))
	counted, err := orm.All(t.Context(), tx, models.Users.Query().With(count).OrderBy(orm.TextAsc(u.Email)))
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
	builder := expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids))
	if updated, err := orm.Update(t.Context(), tx, builder, u.Key(), users[1].ID, orm.NumberSet(u.Age, 25)); err != nil || updated.Age != 25 {
		t.Error("CTE-scoped update failed", err)
	}
	if _, err := orm.Update(t.Context(), tx, builder, u.Key(), users[0].ID, orm.NumberSet(u.Age, 25)); !errors.Is(err, database.NotFound) {
		t.Error("CTE filter did not constrain update", err)
	}
	if _, err := orm.Delete(t.Context(), tx, builder, u.Key(), users[2].ID); err != nil {
		return err
	}
	if n, err := orm.Count(t.Context(), tx, builder); err != nil || n != 1 {
		t.Error("CTE-scoped delete failed", err)
	}
	return nil
}

func TestInvalidCTEsNeverExecute(t *testing.T) {
	for _, definition := range []rowset.CommonTable[models.User]{
		{}, rowset.CTE[models.User]("empty", nil), rowset.CTE("users", rowset.ModelRecord(models.Users.Query())),
		rowset.CTE("bad", rowset.ModelRecord(models.Users.Query().Where(orm.EnumEq(models.Users.Status, models.Status("invalid"))))),
	} {
		source := rowset.AsRecord[eligibleAlias](definition, "eligible")
		ids := rowset.SelectScalar(source, expr.Row(expr.At(rowset.Scope(source), orm.KeyExpr(models.Users.ID))).Value())
		if rows, err := rowset.ReadScalar(ids).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid CTE reached executor", err)
		}
	}
	first := rowset.CTE("same_name", rowset.ModelRecord(models.Users.Query()))
	second := rowset.CTE("same_name", rowset.ModelRecord(models.Users.Query().Where(orm.NumberGt(models.Users.Age, 18))))
	a, b := rowset.AsRecord[eligibleAlias](first, "a"), rowset.AsRecord[otherAlias](second, "b")
	joined := rowset.InnerJoin(a, b, rowset.On(expr.At(rowset.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rowset.Scope(b), orm.KeyExpr(models.Users.ID))))
	ids := rowset.SelectScalar(joined, expr.Row(expr.At(rowset.LeftScope(joined, rowset.Scope(a)), orm.KeyExpr(models.Users.ID))).Value())
	if rows, err := rowset.ReadScalar(ids).All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Error("conflicting CTE definitions reached executor", err)
	}
	// Pin the public result type: CTE IDs remain IDs of the original model.
	var _ rowset.Scalar[rowset.Inner[rowset.Alias[eligibleAlias, models.User], rowset.Alias[otherAlias, models.User]], model.ID[models.User]] = ids
}
