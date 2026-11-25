package tooling

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func routedDatabase(primaryConfig, readConfig postgres.Config, DatabaseProvider foundation.ProviderID, DatabaseKey foundation.Key[*database.DB]) foundation.Module {
	// doc-example:begin database-routing-1
	routing := postgres.RoutingConfig{
		Primary:        primaryConfig,
		Read:           value.Set(readConfig),
		MaxConnections: 24,
	}
	module := postgres.RoutedModule(DatabaseProvider, DatabaseKey, routing)
	// Register module through the ordinary application builder.
	// doc-example:end database-routing-1
	return module
}

func routedReads(ctx context.Context, db *database.DB) error {
	// doc-example:begin database-routing-2
	recent, err := orm.All(ctx, db, models.Users.Query().Limit(10))
	if err != nil {
		return err
	}
	consistent, err := orm.All(ctx, db.Primary(), models.Users.Query().Limit(10))
	if err != nil {
		return err
	}
	// doc-example:end database-routing-2
	_ = recent
	_ = consistent
	return err
}
