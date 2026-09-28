package consumer_test

import (
	"context"
	"testing"

	"foundry.test/consumer/models"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresConsumerAssemblyQueryAndTransaction(t *testing.T) {
	config := pgtest.Config(t)
	key := foundation.NewKey[*database.DB]("consumer.database")
	app := testkit.Start(t, foundry.New().Register(postgres.Module("database", key, config)))
	db, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	var expected int64 = 42
	var actual int64
	if err := database.ScanOne(t.Context(), db, "SELECT $1::bigint", []any{expected}, &actual); err != nil || actual != expected {
		t.Fatalf("consumer parameterized query: %v", err)
	}
	boundStatus, err := models.StatusCodec().Bind(models.StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	var status models.Status
	if err := database.ScanOne(t.Context(), db, "SELECT $1::text", []any{boundStatus}, models.StatusCodec().Scan(&status)); err != nil || status != models.StatusActive {
		t.Fatal("generated enum codec failed against PostgreSQL")
	}
	if err := database.ScanOne(t.Context(), db, "SELECT 'unknown'::text", nil, models.StatusCodec().Scan(&status)); err == nil || status != models.StatusActive {
		t.Fatal("invalid stored enum accepted or changed typed destination")
	}
	committed := false
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		var value int64
		if err := database.ScanOne(t.Context(), tx, "SELECT $1::bigint", []any{expected}, &value); err != nil {
			return err
		}
		if value != expected {
			t.Error("transaction parameter changed")
		}
		return tx.AfterCommit(func(context.Context) error { committed = true; return nil })
	})
	if err != nil || !committed {
		t.Fatalf("consumer transaction: %v", err)
	}
}
