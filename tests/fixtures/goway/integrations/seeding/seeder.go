package seeding

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/seed"
)

// RecordSeeder retains the caller's transaction and normal ORM write pipeline.
func RecordSeeder(name string) seed.Definition {
	return seed.Definition{ID: "fixture.records", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := models.WriteRecords.Insert(ctx, tx, models.WriteRecords.Name.Set(name))
		return err
	}}
}
