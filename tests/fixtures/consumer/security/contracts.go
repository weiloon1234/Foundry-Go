// Package security demonstrates typed outbound, webhook and online-migration APIs.
package security

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

type BillingAccountID int64
type CustomerAccountID int64

func BillingWebhook(account BillingAccountID, key secret.String, source clock.Clock) (*webhook.Verifier[BillingAccountID], error) {
	return webhook.New(webhook.DefaultConfig("billing", webhook.Standard), account, []secret.String{key}, source)
}
func VerifiedAccount(ctx context.Context, verifier *webhook.Verifier[BillingAccountID]) (BillingAccountID, bool) {
	delivery, ok := verifier.FromContext(ctx)
	return delivery.Account(), ok
}
func PreviewSettings() infrastructure.HTTPClientSettings {
	settings := infrastructure.DefaultHTTPClientSettings("preview")
	settings.Config.Destination = httpclient.PublicDestinations()
	settings.Config.Destination.Hosts = []string{"images.example.com"}
	return settings
}
func OnlineMigrations() (*migrate.Registry, error) {
	first := migrate.Key{Origin: "security-consumer", ID: "001_records"}
	return migrate.New(
		migrate.Definition{Key: first, Version: "v1", SQL: []string{"CREATE TABLE consumer_security_records (id bigint PRIMARY KEY, label text)"}},
		migrate.Definition{Key: migrate.Key{Origin: first.Origin, ID: "002_online_label"}, Version: "v1", Requires: []migrate.Key{first}, Mode: migrate.NonTransactional, SQL: []string{"CREATE INDEX CONCURRENTLY consumer_security_label_idx ON consumer_security_records (label)"}},
	)
}
func OnlineRunner(db *database.DB, config migrate.PostgresConfig) (*migrate.Postgres, error) {
	registry, err := OnlineMigrations()
	if err != nil {
		return nil, err
	}
	return migrate.NewPostgres(db, registry, config)
}
func Reconcile(ctx context.Context, runner *migrate.Postgres, inspected migrate.Progress, outcome migrate.StatementOutcome) (migrate.Progress, error) {
	return runner.Reconcile(ctx, inspected, outcome)
}
