package isolatedhttp

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

var Changed = events.Define[Receipt]("scoped.record", 1)
var Rollback = errors.New("requested fixture rollback")

func Defaults() application.Settings {
	s := application.DefaultSettings()
	s.HTTP.Server.Address = "127.0.0.1:0"
	s.Features.Events.Enabled, s.Features.Outbox.Enabled = true, true
	c := infrastructure.DefaultConnectionSettings()
	s.Services.Database.Default = "main"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"main": c, "reporting": c, "archive": c}
	return s
}

func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: "consumer.isolated", ID: "001_records"}, Version: "v1", SQL: []string{
		"CREATE TABLE scope_records (id uuid PRIMARY KEY, name text NOT NULL UNIQUE, text text NOT NULL)",
		"CREATE TABLE scope_deliveries (id uuid PRIMARY KEY, payload jsonb NOT NULL)",
	}}}
}

// Build is ordinary production assembly. Domain handlers receive one actual
// pool and typed inputs; they know nothing about test schemas or test SQL.
func Build(ctx context.Context, s application.Settings, committed func(context.Context, Receipt) error, options ...application.Option) (*application.App, error) {
	return application.New(s, options...).Events(application.Topic(Changed)).Features(func(services application.Services) (application.FeatureDeclarations, error) {
		db, err := services.Database()
		if err != nil {
			return application.FeatureDeclarations{}, err
		}
		return application.FeatureDeclarations{Outbox: []publisher.Route{{Kind: "event", Destination: "scoped", Publish: func(ctx context.Context, message publisher.Message) error {
			payload, err := message.PayloadJSON()
			if err != nil {
				return err
			}
			// An independently committed, duplicate-safe durable test destination.
			_, err = db.Exec(ctx, "INSERT INTO scope_deliveries(id,payload) VALUES($1,$2::jsonb) ON CONFLICT(id) DO NOTHING", message.ID(), payload)
			return err
		}}}}, nil
	}).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		db, err := services.Database()
		if err != nil {
			return nil, err
		}
		bus, err := services.Events()
		if err != nil {
			return nil, err
		}
		producer, err := events.PrepareOutbox("scoped", bus)
		if err != nil {
			return nil, err
		}
		create := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "scoped.create", Method: http.POST, Access: http.Public}, http.StaticPath("/records")), http.EmptyQuery(), http.JSONBody(SubmissionJSON()), http.JSONResponse(201, ReceiptJSON()))
		get := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "scoped.show", Method: http.GET, Access: http.Public}, RecordPathDescriptor()), http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, ReceiptJSON()))
		return []http.RouteRegistration{
			create.Handle(func(ctx context.Context, input http.Input[http.NoPath, http.NoQuery, Submission]) (Receipt, error) {
				var receipt Receipt
				err := db.Transaction(ctx, func(tx *database.Tx) error {
					record, err := QueryScopeRecords().Create(ctx, tx, RecordDraft{}.SetName(input.Body.Name).SetText(input.Body.Text))
					if err != nil {
						return err
					}
					receipt = Receipt{Name: record.Name, Text: record.Text}
					if _, err := Changed.Enqueue(ctx, tx, producer, receipt); err != nil {
						return err
					}
					if committed != nil {
						if err := tx.AfterCommit(func(ctx context.Context) error { return committed(ctx, receipt) }); err != nil {
							return err
						}
					}
					if input.Body.Rollback {
						return Rollback
					}
					return nil
				})
				return receipt, err
			}),
			get.Handle(func(ctx context.Context, input http.Input[RecordPath, http.NoQuery, http.NoBody]) (Receipt, error) {
				record, err := QueryScopeRecords().Where(RecordFields().Name.Eq(input.Path.Name)).RequireFirst(ctx, db)
				return Receipt{Name: record.Name, Text: record.Text}, err
			}),
		}, nil
	}).Build(ctx)
}

func Records(text string) (*factory.Factory[Record, RecordDraft], error) {
	return factory.New[Record](func(context.Context, factory.Sequence) (RecordDraft, error) {
		return RecordDraft{}.SetName("fixture").SetText(text), nil
	})
}
