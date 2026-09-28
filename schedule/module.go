package schedule

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// Module registers one Scheduler kernel and typed service. List the borrowed
// lease manager's provider in requires. Other kernels do not start scheduling.
func Module(name foundation.ProviderID, key foundation.Key[*Scheduler], config Config, requires []foundation.ProviderID, construct func(foundation.Resolver) (*lease.Manager, []Declaration, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return fault.New(fault.Invalid, "scheduler module requires a declaration/lease factory")
		}
		if err := config.Validate(); err != nil {
			return err
		}
		if err := foundation.Factory(r, key, func(resolver foundation.Resolver) (*Scheduler, error) {
			manager, declarations, err := construct(resolver)
			if err != nil {
				return nil, err
			}
			registry, err := NewRegistry(declarations...)
			if err != nil {
				return nil, err
			}
			return New(manager, registry, config)
		}); err != nil {
			return err
		}
		return r.Kernel(foundation.Scheduler, func(runtime *foundation.Runtime) (foundation.Kernel, error) {
			return foundation.Resolve(runtime.Services(), key)
		})
	}}
}
