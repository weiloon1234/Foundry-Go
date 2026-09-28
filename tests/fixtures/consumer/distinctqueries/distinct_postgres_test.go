package distinctqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type personAlias struct{}
type purchaseAlias struct{}
type resultAlias struct{}

func TestPostgresDistinctModelsProjectionsAndValues(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		people := query.As[personAlias](models.QueryUsers(), "person")
		orders := query.As[purchaseAlias](models.QueryOrders(), "purchase")
		p, o := models.UserFieldsAt(people.Scope()), models.OrderFieldsAt(orders.Scope())
		joined := query.InnerJoin(people, orders, query.On(p.ID, o.BuyerID))
		scope := query.LeftScope(joined, people.Scope())
		fields := models.UserFieldsAt(scope)
		base := query.SelectRecord(joined, scope).OrderBy(fields.Email.Asc())
		unique := base.Distinct()
		rows, err := unique.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rows, users[:2]) {
			t.Fatal("distinct did not hydrate complete unique models")
		}
		if n, err := base.Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("distinct mutated base", err)
		}
		if n, err := unique.Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("count lost selected distinct columns", err)
		}
		if n, err := unique.Offset(1).Limit(1).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("distinct count lost window", err)
		}
		if row, err := unique.Offset(1).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[1]) {
			t.Fatal("first paginated duplicate rows", err)
		}
		if row, err := unique.Limit(0).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Fatal("first ignored zero limit", err)
		}
		if found, err := unique.Offset(2).Exists(t.Context(), tx); err != nil || found {
			t.Fatal("exists counted duplicate rows", err)
		}
		var streamed []models.User
		if err := unique.Each(t.Context(), tx, func(u models.User) error { streamed = append(streamed, u); return nil }); err != nil || !reflect.DeepEqual(streamed, users[:2]) {
			t.Fatal("distinct stream changed rows", err)
		}

		// Declared projections retain their aliases, nullable fields and codecs.
		report := reports.ProjectUserSummary(joined).SelectID(fields.ID.Value()).SelectEmail(fields.Email.Value()).SelectNickname(fields.Nickname.Value()).SelectStatus(fields.Status.Value()).Query().Distinct()
		if rows, err := report.All(t.Context(), tx); err != nil || len(rows) != 2 {
			t.Fatal("projection distinct failed", err)
		}
		u := models.UserFields()
		nicknames := query.SelectValue(models.QueryUsers(), u.Nickname.Value()).Distinct().OrderBy(u.Nickname.Asc())
		names, err := nicknames.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(names, []value.Nullable[string]{value.Of("Bee"), value.Null[string]()}) {
			t.Fatal("distinct lost SQL null equality or nullable hydration", names)
		}
		if n, err := nicknames.Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("nullable distinct count changed", err)
		}
		statuses := query.SelectValue(models.QueryUsers(), u.Status.Value()).Distinct()
		if rows, err := statuses.All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []models.Status{models.StatusActive}) {
			t.Fatal("distinct enum type or codec lost", err)
		}
		count := query.Count[models.User]()
		grouped := query.SelectValue(models.QueryUsers(), count.Value()).GroupBy(u.ID.Group()).Having(count.Gt(0)).Distinct().OrderBy(count.Desc())
		if rows, err := grouped.All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []int64{1}) {
			t.Fatal("distinct did not follow grouping/having or order selected aggregate", err)
		}

		// Reusing a parameterized scalar in ORDER BY must refer to its selected value.
		last := query.SelectValue(models.QueryUsers().Where(u.Age.Gte(21)), u.Email.Value()).OrderBy(u.Age.Desc()).Limit(1)
		scalar := query.ScalarQuery(models.QueryUsers(), last)
		constant := query.SelectValue(models.QueryUsers(), scalar).Distinct().OrderBy(scalar.Asc())
		if rows, err := constant.All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, []value.Nullable[string]{value.Of(users[2].Email)}) {
			t.Fatal("parameterized selected ordering failed", err)
		}
		if n, err := constant.Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("scalar distinct count failed", err)
		}

		// CTE dependencies, complete set records and value membership all preserve distinct.
		common := query.CTE("unique_buyers", unique)
		a := query.As[resultAlias](common, "chosen")
		ids := query.SelectValue(a, models.UserFieldsAt(a.Scope()).ID.Value()).Distinct()
		if rows, err := models.QueryUsers().Where(u.ID.InQuery(ids)).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, users[:2]) {
			t.Fatal("distinct CTE membership failed", err)
		}
		combined := unique.UnionAll(models.QueryUsers())
		if n, err := combined.Distinct().Count(t.Context(), tx); err != nil || n != 3 {
			t.Fatal("record set distinct failed", err)
		}
		if n, err := statuses.UnionAll(statuses).Distinct().Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("value set distinct failed", err)
		}
		return nil
	})
}

func TestPostgresDistinctOnSelectsOrderedCompleteRecords(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.UserFields()
		chosen := models.QueryUsers().DistinctOn(u.Status.Group()).OrderBy(u.Status.Asc(), u.Age.Desc(), u.ID.Asc())
		if row, err := chosen.RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[2]) {
			t.Fatal("distinct on did not choose ordered complete model", err)
		}
		if n, err := chosen.Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("distinct on count failed", err)
		}
		o := models.OrderFields()
		latest := models.QueryOrders().DistinctOn(o.BuyerID.Group()).OrderBy(o.BuyerID.Asc(), o.TotalCents.Desc(), o.ID.Asc())
		rows, err := latest.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			t.Fatal("distinct on did not group buyers")
		}
		for _, row := range rows {
			want := int64(2)
			if row.BuyerID == users[1].ID {
				want = 3
			}
			if row.TotalCents != want {
				t.Fatal("distinct on ignored precedence")
			}
		}
		// Keys need not be selected; output still has one declared value type.
		amounts := query.SelectValue(models.QueryOrders(), o.TotalCents.Value()).DistinctOn(o.BuyerID.Group()).OrderBy(o.BuyerID.Asc(), o.TotalCents.Desc())
		if n, err := amounts.Count(t.Context(), tx); err != nil || n != 2 {
			t.Fatal("unselected distinct key rejected", err)
		}
		// A shortened/permuted leftmost key prefix is accepted by PostgreSQL.
		if n, err := models.QueryUsers().DistinctOn(u.Status.Group(), u.Level.Group()).OrderBy(u.Level.Asc()).Count(t.Context(), tx); err != nil || n != 1 {
			t.Fatal("short prefix mismatch with PostgreSQL", err)
		}
		// Outer-owned keys remain correlated and cannot execute separately.
		a := query.As[purchaseAlias](models.QueryOrders(), "purchase")
		link := query.Correlate(models.QueryUsers(), a)
		outer := models.UserFieldsAt(query.OuterScope(link, models.QueryUsers().Scope()))
		inner := models.OrderFieldsAt(query.InnerScope(link, a.Scope()))
		matching := query.SelectCorrelatedValue(link, inner.ID.Value()).Where(inner.BuyerID.EqColumn(outer.ID)).DistinctOn(outer.ID.Group()).OrderBy(outer.ID.Asc(), inner.TotalCents.Desc())
		if rows, err := models.QueryUsers().Where(matching.Exists()).OrderBy(u.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, users[:2]) {
			t.Fatal("correlated distinct key failed", err)
		}
		return nil
	})
}

func TestInvalidDistinctNeverExecutes(t *testing.T) {
	u := models.UserFields()
	for _, q := range []query.ProjectionQuery[models.User, models.User]{
		models.QueryUsers().DistinctOn(),
		models.QueryUsers().DistinctOn(u.Status.Group()).OrderBy(u.Age.Asc()),
		models.QueryUsers().With(models.UserRelations().Orders).Distinct(),
		models.QueryUsers().DistinctOn(u.ID.Group(), u.ID.Group()),
	} {
		if rows, err := q.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid distinct reached executor", err)
		}
	}
	badOrder := query.SelectValue(models.QueryUsers(), u.Email.Value()).Distinct().OrderBy(u.Age.Asc())
	if rows, err := badOrder.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("unselected ordering reached executor", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := models.QueryUsers().Distinct().All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled distinct reached executor", err)
	}
}
