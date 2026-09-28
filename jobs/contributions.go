package jobs

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type jobContribution struct {
	declaration Declaration
	owner       foundation.ProviderID
}
type jobBinding struct{}

func jobContributions(key foundation.Key[*Dispatcher]) foundation.Collection[jobContribution] {
	return foundation.NewCollection[jobContribution](fmt.Sprintf("jobs.declarations.%q", key.Name()))
}

// RegisterJob adds a typed job to a jobs.Module dispatcher. The constructor may
// resolve its own dispatcher; binding occurs in a separate assembly step before
// boot. Use Definition.DeclareWith in the constructor for existing typed job
// middleware and admission. No queue/backend operations run during registration.
func RegisterJob[P any](r *foundation.Registrar, key foundation.Key[*Dispatcher], definition Definition[P], construct func(foundation.Resolver) (Declaration, error)) error {
	if key.Name() == "" || construct == nil || r == nil {
		return fault.New(fault.Invalid, "job contribution requires a dispatcher and constructor")
	}
	if err := definition.Validate(); err != nil {
		return err
	}
	owner := r.Owner()
	return foundation.ContributeAs[P](r, jobContributions(key), fmt.Sprintf("%s.v%d", definition.Name(), definition.Version()), func(resolver foundation.Resolver) (jobContribution, error) {
		dispatcher, err := foundation.Resolve(resolver, key)
		if err != nil {
			return jobContribution{}, err
		}
		if !dispatcher.managed {
			return jobContribution{}, fault.New(fault.Invalid, "job contributions require a jobs Module")
		}
		declaration, err := construct(resolver)
		if err != nil {
			return jobContribution{}, err
		}
		expected := definition.declaration()
		if declaration.key != expected.key || declaration.typ != expected.typ || !declaration.policy.same(expected.policy) {
			return jobContribution{}, fault.New(fault.Invalid, "job contribution returned a different identity, payload type or policy")
		}
		return jobContribution{declaration, owner}, nil
	})
}

func bindContributions(r *foundation.Registrar, key foundation.Key[*Dispatcher]) error {
	owner := r.Owner()
	return foundation.Factory(r, foundation.NewKey[jobBinding](fmt.Sprintf("foundry.jobs.binding.%q", key.Name())), func(resolver foundation.Resolver) (jobBinding, error) {
		dispatcher, err := foundation.Resolve(resolver, key)
		if err != nil {
			return jobBinding{}, err
		}
		contributions, err := foundation.Contributions(resolver, jobContributions(key))
		if err != nil {
			return jobBinding{}, err
		}
		declarations := make([]Declaration, 0, len(dispatcher.registry.entries)+len(contributions))
		owners := make(map[jobKey]foundation.ProviderID)
		for _, declaration := range dispatcher.registry.entries {
			declarations = append(declarations, declaration)
			owners[declaration.key] = owner
		}
		for _, contribution := range contributions {
			if previous, exists := owners[contribution.declaration.key]; exists {
				return jobBinding{}, fault.New(fault.Duplicate, fmt.Sprintf("job %s v%d belongs to both %s and %s", contribution.declaration.key.name, contribution.declaration.key.version, previous, contribution.owner))
			}
			owners[contribution.declaration.key] = contribution.owner
			declarations = append(declarations, contribution.declaration)
		}
		registry, err := NewRegistry(declarations...)
		if err != nil {
			return jobBinding{}, err
		}
		dispatcher.registry = registry
		return jobBinding{}, nil
	})
}
