package distinctqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type personAlias struct{}
type purchaseAlias struct{}
type resultAlias struct{}

func TestPostgresDistinctModelsProjectionsAndValues(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		people := rows.As[personAlias](models.Users.Query(), "person")
		orders := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		p, o := rows.Scope(people), rows.Scope(orders)
		joined := rows.InnerJoin(people, orders, rows.On(expr.At(p, orm.KeyExpr(models.Users.ID)), expr.At(o, orm.ColumnExpr(models.Orders.BuyerID))))
		scope := rows.LeftScope(joined, rows.Scope(people))
		fields := scope
		base := rows.SelectRecord(joined, scope).OrderBy(expr.At(fields, orm.TextExpr(models.Users.Email)).Asc())
		unique := rows.DistinctRecord(base)
		items, err := rows.ReadRecord(unique).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(items, users[:2]) {
			t.Fatal("distinct did not hydrate complete unique models")
		}
		if n, err := rows.ReadRecord(base).Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("distinct mutated base", err)
		}
		if n, err := rows.ReadRecord(unique).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("count lost selected distinct columns", err)
		}
		if n, err := rows.ReadRecord(unique.Offset(1).Limit(1)).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("distinct count lost window", err)
		}
		if row, err := rows.ReadRecord(unique.Offset(1)).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[1]) {
			t.Fatal("first paginated duplicate rows", err)
		}
		if row, err := rows.ReadRecord(unique.Limit(0)).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Fatal("first ignored zero limit", err)
		}
		if found, err := rows.ReadRecord(unique.Offset(2)).Exists(t.Context(), tx); err != nil || found {
			t.Fatal("exists counted duplicate rows", err)
		}

		// Declared projections retain their aliases, nullable fields and codecs.
		report := rows.Distinct(rows.Select(joined, reports.UserSummaries, rows.MapSelected(reports.UserSummaries.ID, expr.Row(expr.At(fields, orm.KeyExpr(models.Users.ID))).Value()), rows.MapSelected(reports.UserSummaries.Email, expr.Row(expr.At(fields, orm.TextExpr(models.Users.Email))).Value()), rows.MapSelected(reports.UserSummaries.Nickname, expr.Row(expr.At(fields, orm.NullableTextExpr(models.Users.Nickname))).Value()), rows.MapSelected(reports.UserSummaries.Status, expr.Row(expr.At(fields, orm.EnumExpr(models.Users.Status))).Value())))
		if items, err := rows.Read(report).All(t.Context(), tx); err != nil || len(items) != 2 {
			t.Fatal("projection distinct failed", err)
		}
		u := models.Users
		nicknames := rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(u.Query()), expr.Row(expr.Value(orm.NullableTextExpr(u.Nickname))).Value())), expr.Value(orm.NullableTextExpr(u.Nickname)).Asc())
		names, err := rows.ReadScalar(nicknames).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(names, []value.Nullable[string]{value.Of("Bee"), value.Null[string]()}) {
			t.Fatal("distinct lost SQL null equality or nullable hydration", names)
		}
		if n, err := rows.ReadScalar(nicknames).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("nullable distinct count changed", err)
		}
		statuses := rows.DistinctScalar(rows.SelectScalar(rows.From(u.Query()), expr.Row(expr.Value(orm.EnumExpr(u.Status))).Value()))
		if items, err := rows.ReadScalar(statuses).All(t.Context(), tx); err != nil || !reflect.DeepEqual(items, []models.Status{models.StatusActive}) {
			t.Fatal("distinct enum type or codec lost", err)
		}
		count := expr.Count[models.User]()
		grouped := rows.OrderScalar(rows.DistinctScalar(rows.HavingScalar(rows.GroupByScalar(rows.SelectScalar(rows.From(u.Query()), count.Value()), expr.Group(expr.Value(orm.KeyExpr(u.ID)))), count.Gt(0))), count.Desc())
		if items, err := rows.ReadScalar(grouped).All(t.Context(), tx); err != nil || !reflect.DeepEqual(items, []int64{1}) {
			t.Fatal("distinct did not follow grouping/having or order selected aggregate", err)
		}

		// Reusing a parameterized scalar in ORDER BY must refer to its selected value.
		last := rows.LimitScalar(rows.OrderScalar(rows.SelectScalar(rows.From(u.Query().Where(orm.NumberGte(u.Age, 21))), expr.Row(expr.Value(orm.TextExpr(u.Email))).Value()), expr.Value(orm.NumberExpr(u.Age)).Desc()), 1)
		scalar := rows.ScalarRowQuery(rows.From(u.Query()), last).Value()
		constant := rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(u.Query()), scalar)), scalar.Asc())
		if items, err := rows.ReadScalar(constant).All(t.Context(), tx); err != nil || !reflect.DeepEqual(items, []value.Nullable[string]{value.Of(users[2].Email)}) {
			t.Fatal("parameterized selected ordering failed", err)
		}
		if n, err := rows.ReadScalar(constant).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("scalar distinct count failed", err)
		}

		// CTE dependencies, complete set records and value membership all preserve distinct.
		common := rows.CTE("unique_buyers", unique)
		a := rows.AsRecord[resultAlias](common, "chosen")
		ids := rows.DistinctScalar(rows.SelectScalar(a, expr.Row(expr.At(rows.Scope(a), orm.KeyExpr(u.ID))).Value()))
		if items, err := orm.All(t.Context(), tx, expr.Where(u.Query(), rows.InQuery(expr.Value(orm.KeyExpr(u.ID)), ids)).OrderBy(orm.TextAsc(u.Email))); err != nil || !reflect.DeepEqual(items, users[:2]) {
			t.Fatal("distinct CTE membership failed", err)
		}
		combined := rows.UnionAll(unique, rows.ModelRecord(u.Query()))
		if n, err := rows.ReadSet(rows.DistinctSet(combined)).Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("record set distinct failed", err)
		}
		if n, err := rows.ReadValues(rows.DistinctValues(rows.UnionAllValues(rows.Values(statuses), rows.Values(statuses)))).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("value set distinct failed", err)
		}
		return nil
	})
}

func TestPostgresDistinctOnSelectsOrderedCompleteRecords(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.Users
		chosen := rows.OrderBy(rows.DistinctOn(rows.FromModel(u.Query()), expr.Group(expr.Value(orm.EnumExpr(u.Status)))), expr.Value(orm.EnumExpr(u.Status)).Asc(), expr.Value(orm.NumberExpr(u.Age)).Desc(), expr.Value(orm.KeyExpr(u.ID)).Asc())
		if row, err := rows.Read(chosen).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[2]) {
			t.Fatal("distinct on did not choose ordered complete model", err)
		}
		if n, err := rows.Read(chosen).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("distinct on count failed", err)
		}
		o := models.Orders
		latest := rows.OrderBy(rows.DistinctOn(rows.FromModel(o.Query()), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.Value(orm.ColumnExpr(o.BuyerID)).Asc(), expr.Value(orm.NumberExpr(o.TotalCents)).Desc(), expr.Value(orm.KeyExpr(o.ID)).Asc())
		items, err := rows.Read(latest).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			t.Fatal("distinct on did not group buyers")
		}
		for _, row := range items {
			want := int64(2)
			if row.BuyerID == users[1].ID {
				want = 3
			}
			if row.TotalCents != want {
				t.Fatal("distinct on ignored precedence")
			}
		}
		// Keys need not be selected; output still has one declared value type.
		amounts := rows.OrderScalar(rows.DistinctOnScalar(rows.SelectScalar(rows.From(o.Query()), expr.Row(expr.Value(orm.NumberExpr(o.TotalCents))).Value()), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.Value(orm.ColumnExpr(o.BuyerID)).Asc(), expr.Value(orm.NumberExpr(o.TotalCents)).Desc())
		if n, err := rows.ReadScalar(amounts).Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("unselected distinct key rejected", err)
		}
		// A shortened/permuted leftmost key prefix is accepted by PostgreSQL.
		if n, err := rows.Read(rows.OrderBy(rows.DistinctOn(rows.FromModel(u.Query()), expr.Group(expr.Value(orm.EnumExpr(u.Status))), expr.Group(expr.Value(orm.EnumExpr(u.Level)))), expr.Value(orm.EnumExpr(u.Level)).Asc())).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("short prefix mismatch with PostgreSQL", err)
		}

		return nil
	})
}

func TestInvalidDistinctNeverExecutes(t *testing.T) {
	u := models.Users
	for _, q := range []rows.Query[models.User, models.User]{
		rows.DistinctOn(rows.FromModel(u.Query())),
		rows.OrderBy(rows.DistinctOn(rows.FromModel(u.Query()), expr.Group(expr.Value(orm.EnumExpr(u.Status)))), expr.Value(orm.NumberExpr(u.Age)).Asc()),
		rows.Distinct(rows.FromModel(u.Query().With(models.UserLinks.Orders))),
		rows.DistinctOn(rows.FromModel(u.Query()), expr.Group(expr.Value(orm.KeyExpr(u.ID))), expr.Group(expr.Value(orm.KeyExpr(u.ID)))),
	} {
		if items, err := rows.Read(q).All(t.Context(), queryfixture.NoQueries(t)); items != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid distinct reached executor", err)
		}
	}
	badOrder := rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(u.Query()), expr.Row(expr.Value(orm.TextExpr(u.Email))).Value())), expr.Value(orm.NumberExpr(u.Age)).Asc())
	if items, err := rows.ReadScalar(badOrder).All(t.Context(), queryfixture.NoQueries(t)); items != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("unselected ordering reached executor", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if items, err := rows.Read(rows.Distinct(rows.FromModel(u.Query()))).All(ctx, queryfixture.NoQueries(t)); items != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled distinct reached executor", err)
	}
}

func TestPostgresDistinctRecordStreaming(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		people := rows.As[personAlias](models.Users.Query(), "person")
		orders := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		p, o := rows.Scope(people), rows.Scope(orders)
		joined := rows.InnerJoin(people, orders, rows.On(expr.At(p, orm.KeyExpr(models.Users.ID)), expr.At(o, orm.ColumnExpr(models.Orders.BuyerID))))
		scope := rows.LeftScope(joined, p)
		unique := rows.DistinctRecord(rows.SelectRecord(joined, scope).OrderBy(expr.At(scope, orm.TextExpr(models.Users.Email)).Asc()))
		var streamed []models.User
		if err := rows.EachRead(t.Context(), tx, unique, func(u models.User) error { streamed = append(streamed, u); return nil }); err != nil || !reflect.DeepEqual(streamed, users[:2]) {
			t.Fatal("distinct stream changed rows", err)
		}
		return nil
	})
}

func TestPostgresCorrelatedDistinctOn(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := models.Users.Query()
		a := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		link := rows.Correlate(rows.From(q), a)
		outer := expr.At(rows.OuterScope(link, rows.ModelScope(q)), orm.KeyExpr(models.Users.ID))
		inner := rows.InnerScope(link, rows.Scope(a))
		matching := rows.SelectCorrelatedScalar(link, expr.Row(expr.At(inner, orm.KeyExpr(models.Orders.ID))).Value()).Where(expr.Equal(expr.At(inner, orm.ColumnExpr(models.Orders.BuyerID)), outer))
		matching = rows.DistinctOnCorrelatedScalar(matching, expr.Group(outer)).OrderBy(outer.Asc(), expr.At(inner, orm.NumberExpr(models.Orders.TotalCents)).Desc())
		got, err := orm.All(t.Context(), tx, expr.Where(q, matching.Exists()).OrderBy(orm.TextAsc(models.Users.Email)))
		if err != nil || !reflect.DeepEqual(got, users[:2]) {
			t.Fatal("correlated distinct key failed", err)
		}
		return nil
	})
}
