package tooling

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

// doc-example:begin countries-1
func seedCountries(ctx context.Context, store *extensions.Store, database *database.DB) error {
	// Register countries.Migrations() in the ordinary migration registry first.
	seeders, err := seed.New(countries.Seeder(store))
	if err != nil {
		return err
	}
	result, err := seeders.Run(ctx, database, countries.SeederID)
	if err != nil {
		return err
	}

	code, err := countries.ParseCode("my")
	if err != nil {
		return err
	}
	malaysia, err := countries.Find(ctx, store, code)
	if err != nil {
		return err
	}
	_ = result
	_ = malaysia
	return err
}

// doc-example:end countries-1
