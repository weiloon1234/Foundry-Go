package security

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPublicPreviewConfiguration(t *testing.T) {
	settings := PreviewSettings()
	client, err := httpclient.New(settings.Config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())
	if client.Get("https://127.0.0.1/").Validate() == nil {
		t.Fatal("private destination accepted")
	}
	if err := client.Get("https://images.example.com/image").Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestPublicConcurrentIndexConsumer(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	config := migrate.DefaultPostgresConfig()
	config.Schema = scope.Schema()
	runner, err := OnlineRunner(db, config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Up(t.Context())
	if err != nil || len(result.Applied) != 2 {
		t.Fatal(result, err)
	}
	var valid bool
	if err := database.ScanOne(t.Context(), db, "SELECT indisvalid FROM pg_catalog.pg_index WHERE indexrelid='consumer_security_label_idx'::regclass", nil, &valid); err != nil || !valid {
		t.Fatal("public runner failed concurrent index", err)
	}
	report, err := runner.Status(t.Context())
	if err != nil || report.Check() != nil || report.Statuses[1].State != migrate.Complete {
		t.Fatal(err)
	}
}
