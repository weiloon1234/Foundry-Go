package idempotenthttp

import (
	"context"
	"math"
	"strings"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var Created = events.Define[Receipt]("idempotent.order", 1)
var Rejected = http.DefineError("order_rejected", 422, "The order was rejected.")

type Request = http.Input[Path, http.NoQuery, Submission]
type BoundRequest = modelbinding.Input[Path, http.NoQuery, Submission, Workspace]
type Hooks struct {
	Deny          *atomic.Bool
	Calls         *atomic.Int32
	InTransaction func(context.Context, *database.Tx, BoundRequest) error
	AfterCommit   func(context.Context, Receipt) error
}

func Defaults() application.Settings {
	settings := application.DefaultSettings()
	settings.HTTP.Server.Address = "127.0.0.1:0"
	settings.Features.Events.Enabled = true
	settings.Features.Outbox.Enabled = true
	settings.Features.Idempotency.Enabled = true
	settings.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	return settings
}
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: "consumer.idempotent", ID: "001_orders"}, Version: "v1", SQL: []string{
		`CREATE TABLE idem_workspaces(id bigint PRIMARY KEY,tenant text NOT NULL,enabled boolean NOT NULL)`,
		`CREATE TABLE idem_orders(id uuid PRIMARY KEY,workspace_id bigint NOT NULL REFERENCES idem_workspaces(id),caller bigint NOT NULL,name text NOT NULL)`,
		`CREATE TABLE idem_deliveries(id uuid PRIMARY KEY,payload jsonb NOT NULL)`,
		`INSERT INTO idem_workspaces(id,tenant,enabled) VALUES(1,'tenant-a',true),(2,'tenant-b',true)`,
	}}}
}
func Endpoint() http.Endpoint[Path, http.NoQuery, Submission, Receipt] {
	return http.ValidateBody(http.PrepareEndpoint(http.ConfigureEndpoint(http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "orders.create", Method: http.POST, Access: http.Guarded}, PathDescriptor()), http.EmptyQuery(), http.JSONBody(SubmissionJSON()), http.JSONResponse(201, ReceiptJSON())), http.Errors(Rejected, OrderExists)), func(_ context.Context, in Request) (http.NoQuery, Submission, error) {
		in.Body.Name = strings.TrimSpace(in.Body.Name)
		return in.Query, in.Body, nil
	}), validation.Rules(validation.DefineField("name", func(in Submission) string { return in.Name }), validation.NonBlank[string]()))
}

// Build uses ordinary configured infrastructure. Only the idempotent callback's
// supplied transaction owns business writes and existing outbox enqueue.
func Build(ctx context.Context, settings application.Settings, hooks Hooks, options ...application.Option) (*application.App, error) {
	return application.New(settings, options...).Events(application.Topic(Created)).Features(func(services application.Services) (application.FeatureDeclarations, error) {
		db, err := services.Database()
		if err != nil {
			return application.FeatureDeclarations{}, err
		}
		return application.FeatureDeclarations{Outbox: []publisher.Route{{Kind: "event", Destination: "orders", Publish: func(ctx context.Context, message publisher.Message) error {
			payload, err := message.PayloadJSON()
			if err != nil {
				return err
			}
			_, err = db.Exec(ctx, `INSERT INTO idem_deliveries(id,payload) VALUES($1,$2::jsonb) ON CONFLICT(id) DO NOTHING`, message.ID(), payload)
			return err
		}}}}, nil
	}).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		db, err := services.Database()
		if err != nil {
			return nil, err
		}
		store, err := services.Idempotency()
		if err != nil {
			return nil, err
		}
		bus, err := services.Events()
		if err != nil {
			return nil, err
		}
		producer, err := events.PrepareOutbox("orders", bus)
		if err != nil {
			return nil, err
		}
		transport, guard, err := Authentication()
		if err != nil {
			return nil, err
		}
		required := http.AuthorizeActor(http.RequireAuthentication(Endpoint(), transport, guard), func(_ context.Context, _ Actor, in Request) error {
			if in.Body.Name == "request-denied" || hooks.Deny != nil && hooks.Deny.Load() {
				return http.Forbidden
			}
			return nil
		})
		return []http.RouteRegistration{OrderRoute(db, required, guard, store, func(ctx context.Context, tx *database.Tx, actor Actor, in BoundRequest) (Receipt, error) {
			if hooks.Calls != nil {
				hooks.Calls.Add(1)
			}
			receipt, err := CreateOrder(ctx, tx, actor, in, producer)
			if err != nil {
				return Receipt{}, err
			}
			if hooks.InTransaction != nil {
				if err := hooks.InTransaction(ctx, tx, in); err != nil {
					return Receipt{}, err
				}
			}
			if hooks.AfterCommit != nil {
				if err := tx.AfterCommit(func(ctx context.Context) error { return hooks.AfterCommit(ctx, receipt) }); err != nil {
					return Receipt{}, err
				}
			}
			switch in.Request.Body.Failure {
			case "business":
				return Receipt{}, Rejected
			case "encode":
				receipt.Score = math.NaN()
			case "oversized":
				receipt.Name = strings.Repeat("x", settings.Features.Idempotency.Config.MaxResultBytes)
			}
			return receipt, nil
		})}, nil
	}).Build(ctx)
}
