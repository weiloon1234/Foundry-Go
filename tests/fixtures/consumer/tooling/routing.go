package tooling

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/value"
)

func RoutedDatabase(name foundation.ProviderID, key foundation.Key[*database.DB], primary, read postgres.Config, maxConnections int) foundation.Module {
	return postgres.RoutedModule(name, key, postgres.RoutingConfig{Primary: primary, Read: value.Set(read), MaxConnections: maxConnections})
}

func ReadUsers(ctx context.Context, db *database.DB) ([]models.User, error) {
	return orm.All(ctx, db, models.Users.Query().Limit(10))
}

func ReadPrimaryUsers(ctx context.Context, db *database.DB) ([]models.User, error) {
	return orm.All(ctx, db.Primary(), models.Users.Query().Limit(10))
}

func DatabaseEndpoints(ctx context.Context, db *database.DB) ([]database.PoolHealth, database.RoutingStats) {
	return db.Health(ctx), db.RoutingStats()
}

func DatabaseReadiness(name foundation.ProviderID, key foundation.Key[*health.Registry], dependency foundation.ProviderID, databaseKey foundation.Key[*database.DB]) foundation.Module {
	return health.Module(name, key, health.DefaultConfig(), []foundation.ProviderID{dependency}, func(resolver foundation.Resolver) ([]health.Probe, error) {
		db, err := foundation.Resolve(resolver, databaseKey)
		if err != nil {
			return nil, err
		}
		return db.ReadinessProbes("database.primary", "database.read"), nil
	})
}
