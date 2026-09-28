package application

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type IdempotencySettings struct {
	Enabled  bool
	Database database.ConnectionName
	Config   idempotency.Config
}

func DefaultIdempotencySettings() IdempotencySettings {
	return IdempotencySettings{Config: idempotency.DefaultConfig()}
}

const IdempotencyProvider foundation.ProviderID = "foundry.application.idempotency"

var IdempotencyKey = foundation.NewKey[*idempotency.Store](string(IdempotencyProvider))

func registerIdempotency(builder *foundation.Builder, settings IdempotencySettings, namespace keyspace.Namespace) {
	if !settings.Enabled {
		return
	}
	builder.Register(idempotency.Module(IdempotencyProvider, IdempotencyKey, []foundation.ProviderID{infrastructure.DatabaseProvider(settings.Database)}, func(r foundation.Resolver) (*idempotency.Store, error) {
		db, err := foundation.Resolve(r, infrastructure.DatabaseKey(settings.Database))
		if err != nil {
			return nil, err
		}
		return idempotency.New(db, namespace, settings.Config)
	}))
}
func (s Services) Idempotency() (*idempotency.Store, error) { return Resolve(s, IdempotencyKey) }
