package consumer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Exercise observer construction, injected defaults and automatic ORM dispatch.
func TestPostgresObserverOwnershipFromProviders(t *testing.T) {
	pool := foundation.NewKey[*database.DB]("observer.database")
	defaultEmail := foundation.NewKey[string]("observer.default-email")
	var constructed int
	var instantiated []string
	register := func(r *foundation.Registrar, name string) error {
		observer := orm.NewObserver[models.User](name)
		return orm.RegisterObserver(r, pool, observer, func(resolver foundation.Resolver) (func() orm.Hooks[models.User], error) {
			constructed++
			email, err := foundation.Resolve(resolver, defaultEmail)
			if err != nil {
				return nil, err
			}
			return func() orm.Hooks[models.User] {
				instantiated = append(instantiated, name)
				return orm.Hooks[models.User]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[models.User]) error {
					return orm.WriteSet(draft, models.Users.Email.Set(email))
				}}
			}, nil
		})
	}
	app := testkit.Start(t, foundry.New().Register(
		foundation.Module{Name: "observer.second", Requires: []foundation.ProviderID{"observer.first"}, OnRegister: func(r *foundation.Registrar) error { return register(r, "second") }},
		postgres.Module("database", pool, pgtest.Config(t)),
		foundation.Module{Name: "observer.first", OnRegister: func(r *foundation.Registrar) error {
			if err := foundation.Provide(r, defaultEmail, "default@example.test"); err != nil {
				return err
			}
			return register(r, "first")
		}},
	))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if constructed != 2 || len(instantiated) != 0 {
		t.Fatal("bootstrap invoked write factories or repeated dependency construction")
	}
	namespace := pgtest.Namespace(t, db)
	if _, err := db.Exec(t.Context(), `CREATE TABLE "`+namespace+`".users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`); err != nil {
		t.Fatal(err)
	}
	check := func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		item, err := models.Users.Insert(t.Context(), tx, models.Users.Email.Set("original"), models.Users.Age.Set(0), models.Users.Status.Set(models.StatusActive), models.Users.Level.Set(models.LevelBasic))
		if err != nil {
			return err
		}
		if item.Email != "default@example.test" {
			return errors.New("typed injected dependency missing")
		}
		return nil
	}

	if err := db.Transaction(t.Context(), func(tx *database.Tx) error { return tx.Savepoint(t.Context(), check) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(s *database.Session) error { return s.Transaction(t.Context(), check) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(instantiated, []string{"first", "second", "first", "second"}) {
		t.Fatal("factories lost ordering or operation state", instantiated)
	}
	if constructed != 2 {
		t.Fatal("request path resolved application dependencies")
	}
}
