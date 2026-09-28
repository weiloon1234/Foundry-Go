package consumer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// This fixture checks observer construction and ownership, not automatic model
// callback dispatch. The generated adapter integration is the next increment.
func TestPostgresObserverOwnershipFromProviders(t *testing.T) {
	pool := foundation.NewKey[*database.DB]("observer.database")
	defaultEmail := foundation.NewKey[string]("observer.default-email")
	var constructed int
	var instantiated []string
	register := func(r *foundation.Registrar, name string) error {
		observer := lifecycle.NewObserver[models.User, models.UserHooks](name)
		return database.RegisterObserver(r, pool, observer, func(resolver foundation.Resolver) (func() models.UserHooks, error) {
			constructed++
			email, err := foundation.Resolve(resolver, defaultEmail)
			if err != nil {
				return nil, err
			}
			return func() models.UserHooks {
				instantiated = append(instantiated, name)
				return models.UserHooks{Creating: func(_ context.Context, _ *database.Tx, draft *models.UserDraft) error {
					*draft = draft.SetEmail(email)
					return nil
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
	check := func(tx *database.Tx) error {
		factories, err := lifecycle.ObserverFactories[models.User, models.UserHooks](tx.Observers())
		if err != nil {
			return err
		}
		if len(factories) != 2 {
			return errors.New("transaction lost observers")
		}
		var draft models.UserDraft
		for _, factory := range factories {
			// Invoke the typed callback directly to check the injected dependency.
			// This is not an assertion about automatic Create/Update dispatch.
			if err := factory().Creating(t.Context(), tx, &draft); err != nil {
				return err
			}
		}
		if got, ok := draft.Email().Get(); !ok || got != "default@example.test" {
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
