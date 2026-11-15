package application

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

type historyApplication struct {
	mockApplication
	history []infrastructure.MigrationGroup
}

func (a *historyApplication) MigrationHistory() []infrastructure.MigrationGroup { return a.history }

func TestCompleteHistoryKeepsTestNamespaceBoundary(t *testing.T) {
	env := makeTestEnvironment(t)
	d := migrate.Definition{Key: migrate.Key{Origin: "foundry.history", ID: "001"}, Version: "v1", SQL: []string{"SELECT 1"}, Availability: migrate.FeatureDisabled}
	app := &historyApplication{history: []infrastructure.MigrationGroup{
		{Connections: []database.ConnectionName{"main"}, Schema: "retained_scope", Definitions: []migrate.Definition{d}},
		{Connections: []database.ConnectionName{"main"}, Schema: "public", Definitions: []migrate.Definition{d}},
	}}
	targets, err := env.applicationTargets(app)
	if err != nil || len(targets) != 1 || targets[0].Schema != "retained_scope" {
		t.Fatal("disabled-only owned history missing or unowned namespace selected", targets, err)
	}
	app.history[1].Definitions[0].Availability = migrate.Available
	targets, err = env.applicationTargets(app)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.migrationGroups(targets); !errors.Is(err, fault.Invalid) {
		t.Fatal("runnable history escaped namespace", err)
	}
	// Explicit domain declarations are still rejected, even when disabled.
	if _, err := env.migrationGroups([]infrastructure.MigrationTarget{{Schema: "public", Definitions: []migrate.Definition{d}}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("explicit target escaped namespace", err)
	}
}
