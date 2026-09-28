package audit

import (
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Register constructs one recorder and adds generated model audit observers to
// the selected pool. Foundation owns duplicate detection, dependency resolution
// and freezing; model callbacks retain the constructed recorder directly.
func Register(r *foundation.Registrar, pool foundation.Key[*database.DB], key foundation.Key[*Recorder], config Config, models ...record.Declaration) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := foundation.Factory(r, key, func(foundation.Resolver) (*Recorder, error) { return New(config) }); err != nil {
		return err
	}
	for _, declaration := range models {
		if err := declaration.Register(r, pool, func(resolver foundation.Resolver) (record.Writer, error) {
			return foundation.Resolve(resolver, key)
		}); err != nil {
			return err
		}
	}
	return nil
}
