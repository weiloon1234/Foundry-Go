package consumer_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresGeneratedRelations(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
			`CREATE TABLE orders (id uuid PRIMARY KEY, buyer_id uuid NOT NULL, total_cents bigint NOT NULL)`,
			`CREATE TABLE countries (code text PRIMARY KEY, name text NOT NULL)`,
			`CREATE TABLE locations (code text PRIMARY KEY, country_code text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		var users []models.User
		for i := 0; i < 4; i++ {
			draft := models.UserDraft{}.SetEmail("fixture").SetAge(i + 20).SetStatus(models.StatusActive).SetLevel(models.LevelBasic)
			if i == 1 {
				draft = draft.SetStatus(models.StatusDisabled)
			}
			if i == 1 || i == 2 {
				draft = draft.SetIntroducerID(users[0].ID)
			}
			if i == 3 {
				draft = draft.SetIntroducerID(users[1].ID)
			}
			user, err := models.QueryUsers().Create(t.Context(), tx, draft)
			if err != nil {
				return err
			}
			users = append(users, user)
		}
		for i, buyer := range []model.ID[models.User]{users[0].ID, users[0].ID, users[1].ID} {
			if _, err := models.QueryOrders().Create(t.Context(), tx, models.OrderDraft{}.SetBuyerID(buyer).SetTotalCents(int64(i+1))); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		base := models.QueryUsers().OrderBy(models.UserFields().Age.Asc())
		loaded, err := base.With(models.UserRelations().Introducer).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 2 || len(loaded) != 4 {
			t.Error("belongs-to did not use one batched related query")
		}
		for i, user := range loaded {
			introducer, isLoaded := user.Introducer.Get()
			if !isLoaded || users[i].Introducer.IsLoaded() {
				t.Error("loaded state leaked into caller models")
			}
			v, present := introducer.Get()
			if i == 0 && present {
				t.Error("nullable belongs-to did not load empty")
			}
			if i > 0 {
				expected := users[0].ID
				if i == 3 {
					expected = users[1].ID
				}
				if !present || v.ID != expected {
					t.Error("belongs-to attached wrong key")
				}
			}
			if user.Orders.IsLoaded() {
				t.Error("unrequested relation loaded")
			}
		}
		counter.Queries.Store(0)
		if _, err := models.QueryUsers().With(models.UserRelations().Introducer).LoadMissing(t.Context(), counter, loaded); err != nil {
			return err
		}
		if counter.Queries.Load() != 0 {
			t.Error("LoadMissing queried already loaded/empty slots")
		}
		counter.Queries.Store(0)
		if _, err := models.QueryUsers().With(models.UserRelations().Introducer).Load(t.Context(), counter, []models.User{users[1], users[1]}); err != nil {
			return err
		}
		if counter.Queries.Load() != 1 {
			t.Error("duplicate parent keys were not batched")
		}
		limits := query.DefaultRelationLimits()
		limits.BatchSize = 2
		counter.Queries.Store(0)
		collections, err := base.WithRelationLimits(limits).With(models.UserRelations().Orders.OrderBy(models.OrderFields().TotalCents.Desc())).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 3 {
			t.Error("configured key chunks did not bound query count")
		}
		for i, user := range collections {
			orders, isLoaded := user.Orders.Get()
			if !isLoaded {
				t.Error("collection not marked loaded")
			}
			count := 0
			if i == 0 {
				count = 2
			}
			if i == 1 {
				count = 1
			}
			if len(orders) != count {
				t.Error("has-many attached wrong group")
			}
			if i == 0 && len(orders) == 2 && orders[0].TotalCents <= orders[1].TotalCents {
				t.Error("relation order was lost")
			}
		}
		counter.Queries.Store(0)
		nested, err := base.With(models.UserRelations().Introducer.With(models.UserRelations().Introducer)).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 3 {
			t.Error("nested eager loading used per-parent queries")
		}
		parent, _ := nested[3].Introducer.Get()
		p, _ := parent.Get()
		grandparent, _ := p.Introducer.Get()
		g, ok := grandparent.Get()
		if !ok || g.ID != users[0].ID {
			t.Error("nested self-relation lost target")
		}
		referrals, err := base.With(models.UserRelations().Referrals).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for i, user := range referrals {
			items, loaded := user.Referrals.Get()
			if !loaded || len(items) != []int{2, 1, 0, 0}[i] {
				t.Error("nullable inverse key attached wrong referrals")
			}
		}
		scoped, err := base.With(models.UserRelations().Introducer.Where(models.UserFields().Status.Eq(models.StatusActive))).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if parent, _ := scoped[3].Introducer.Get(); parent.IsSet() {
			t.Error("related query ignored explicit scope")
		}
		if result, err := base.With(models.UserRelations().SingleOrder).All(t.Context(), tx); !errors.Is(err, database.TooManyRows) || result != nil {
			t.Error("has-one selected an arbitrary duplicate")
		}
		one, err := base.With(models.UserRelations().SingleOrder.Where(models.OrderFields().TotalCents.Gt(1))).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if v, loaded := one[0].SingleOrder.Get(); !loaded || !v.IsSet() {
			t.Error("scoped has-one did not load")
		}
		counter.Queries.Store(0)
		badDepth := models.UserRelations().Introducer.With(models.UserRelations().Introducer)
		limits.MaxDepth = 1
		if _, err := base.WithRelationLimits(limits).With(badDepth).All(t.Context(), counter); !errors.Is(err, fault.Invalid) {
			t.Error("depth bound ignored")
		}
		if _, err := base.With(models.UserRelations().Introducer, models.UserRelations().Introducer).All(t.Context(), counter); !errors.Is(err, fault.Invalid) {
			t.Error("duplicate slot accepted")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := base.With(models.UserRelations().Introducer).All(ctx, counter); !errors.Is(err, context.Canceled) {
			t.Error("canceled eager query executed")
		}
		if counter.Queries.Load() != 0 {
			t.Error("invalid graph reached parent SQL")
		}
		limits = query.DefaultRelationLimits()
		limits.MaxRows = 1
		if result, err := base.WithRelationLimits(limits).With(models.UserRelations().Introducer).All(t.Context(), tx); !errors.Is(err, fault.Invalid) || result != nil {
			t.Error("row budget returned partial models")
		}
		// Failure after an earlier branch loaded must not mutate supplied parents.
		if result, err := base.With(models.UserRelations().Introducer, models.UserRelations().SingleOrder).Load(t.Context(), tx, users); !errors.Is(err, database.TooManyRows) || result != nil {
			t.Error("multi-branch failure published a partial load")
		}
		for _, user := range users {
			if user.Introducer.IsLoaded() {
				t.Error("failed Load mutated its source")
			}
		}
		limits = query.DefaultRelationLimits()
		limits.MaxRows = 3
		if result, err := models.QueryUsers().WithRelationLimits(limits).With(models.UserRelations().Orders).Load(t.Context(), tx, []models.User{users[0], users[0]}); !errors.Is(err, fault.Invalid) || result != nil {
			t.Error("duplicate parent expansion bypassed the relation budget")
		}
		for _, code := range []models.CountryCode{"MY", "SG"} {
			if _, err := models.QueryCountries().Create(t.Context(), tx, models.CountryDraft{}.SetCode(code).SetName(string(code))); err != nil {
				return err
			}
		}
		for _, code := range []models.LocationCode{"KL", "JB"} {
			if _, err := models.QueryLocations().Create(t.Context(), tx, models.LocationDraft{}.SetCode(code).SetCountryCode("MY")); err != nil {
				return err
			}
		}
		countries, err := models.QueryCountries().OrderBy(models.CountryFields().Code.Asc()).With(models.CountryRelations().Locations.With(models.LocationRelations().Country)).All(t.Context(), tx)
		if err != nil {
			return err
		}
		locations, isLoaded := countries[0].Locations.Get()
		if !isLoaded || len(locations) != 2 {
			t.Error("natural-key has-many failed")
		}
		for _, location := range locations {
			country, _ := location.Country.Get()
			v, ok := country.Get()
			if !ok || v.Code != "MY" {
				t.Error("natural-key inverse failed")
			}
		}
		if empty, loaded := countries[1].Locations.Get(); !loaded || len(empty) != 0 {
			t.Error("empty natural-key collection is not loaded")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
