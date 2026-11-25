package tooling

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
)

// doc-example:begin database-runtime-1
func assertOperationShapes(t *testing.T, config postgres.Config, run func(context.Context, *database.DB) error) {
	t.Helper()
	detector, err := database.NewStatementDetector(database.DefaultStatementDetectorConfig())
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(t.Context(), config, database.WithStatementDetector(detector))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	ctx, scope := detector.Begin(t.Context())
	// The operation closes its rows before returning so the scope is complete.
	if err := run(ctx, db); err != nil {
		t.Fatal(err)
	}
	dbassert.AssertStatementShapes(t, scope)
}

// doc-example:end database-runtime-1
