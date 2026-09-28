package publisher

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Module starts one managed publisher after its borrowed database and queue
// providers boot. Foundation drains it before shutting either dependency down.
func Module(name foundation.ProviderID, key foundation.Key[*Publisher], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Publisher, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "outbox module requires a publisher factory")
		}
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		publisher, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		return r.Go("outbox", publisher.Run)
	}}
}
