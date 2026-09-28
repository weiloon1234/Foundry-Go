package application

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// WithDatabaseScopes snapshots consumer settings and aligns every declared
// persistence feature with its selected scoped connection. It preserves named
// defaults and rejects incompatible explicit schemas before application Build.
// Blank/public feature schemas follow the connection; custom schemas must match.
func (s Settings) WithDatabaseScopes(databases infrastructure.DatabaseSettings) (Settings, error) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		return Settings{}, err
	}
	result, _, err := schema.Load(s, config.Inputs[Settings]{})
	if err != nil {
		return Settings{}, err
	}
	result.Services, err = result.Services.WithDatabaseScopes(databases)
	if err != nil {
		return Settings{}, err
	}
	for _, target := range persistenceTargets(&result.Features) {
		name, scope, err := databases.ScopedSchema(*target.connection)
		if err != nil {
			return Settings{}, err
		}
		if *target.schema != "" && *target.schema != "public" && *target.schema != scope {
			return Settings{}, fault.New(fault.Invalid, "feature schema conflicts with database scope")
		}
		*target.connection, *target.schema = name, scope
	}
	return result, nil
}
