package notifying_test

import (
	"context"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPublicNotificationConsumer(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, migration := range notifications.Migrations() {
			for _, statement := range migration.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Email: "fixture@example.test", Status: models.StatusActive}
	provider := auth.DefineProvider("users", models.User{}.FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		if key == id {
			return value.Set(user), nil
		}
		return value.Optional[models.User]{}, nil
	}, func(_ context.Context, u models.User) (bool, error) { return u.Status == models.StatusActive, nil })
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("fixture", func(context.Context, secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		return value.Set(proof), nil
	})
	guard := auth.DefineGuard("web", provider, strategy)
	recipient := notifying.Users(provider, guard)
	binding := notifications.Bind(notifying.Placed, recipient, notifying.Database.Channel())
	config := application.DefaultSettings()
	config.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	config.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	config.Features.Notifications.Enabled = true
	config.Features.Notifications.Schema = schema
	app, err := application.New(config).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Notifications: []notifications.Registration{binding.Registration()}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	manager, err := app.Resources().Notifications()
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := model.NewID[models.Order]()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := notifying.Capture(t.Context(), binding, user, notifying.OrderPlaced{OrderID: orderID, Summary: "consumer message"})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := pending.Send(t.Context(), manager); err != nil || report.Retryable() {
		t.Fatal("notification send", err)
	}
	authentication, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "fixture", Secret: secret.New("test")})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := authentication.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	inbox, err := recipient.Inbox(manager)
	if err != nil {
		t.Fatal(err)
	}
	page, err := notifying.Unread(scope.Context(), inbox, query.PageRequest{Number: 1, Size: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("recipient inbox", err)
	}
	card, err := notifying.Database.Decode(scope.Context(), notifying.Placed, page.Items[0])
	if err != nil || card.OrderID != orderID || card.Summary != "consumer message" {
		t.Fatal("typed inbox data", err)
	}
	if found, err := inbox.MarkRead(scope.Context(), pending.ID()); err != nil || !found {
		t.Fatal(err)
	}
}
