package auth

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type authorizationDeclaration interface {
	Registration() Registration
	Validate() error
}
type registryRegistration struct{}

func registryRegistrationKey(key foundation.Key[*Registry]) foundation.Key[registryRegistration] {
	return foundation.NewKey[registryRegistration](fmt.Sprintf("foundry.auth.registry.%q", key.Name()))
}

func authorizationContributions(key foundation.Key[*Registry]) foundation.Collection[Registration] {
	return foundation.NewCollection[Registration](fmt.Sprintf("auth.declarations.%q", key.Name()))
}

// RegisterAuthorization adds the existing typed guard or policy declaration to
// an application registry. Its subject/resource types remain part of override
// validation, and its exact declaration identity is retained for auth checks.
func RegisterAuthorization[D authorizationDeclaration](r *foundation.Registrar, key foundation.Key[*Registry], declaration D) error {
	if key.Name() == "" || r == nil {
		return fault.New(fault.Invalid, "authorization contribution requires a registry and registrar")
	}
	if err := declaration.Validate(); err != nil {
		return err
	}
	item := declaration.Registration()
	item.owner = r.Owner()
	kind := "guard."
	if item.kind == policyRegistration {
		kind = "policy."
	}
	return foundation.ContributeAs[D](r, authorizationContributions(key), kind+item.name, func(resolver foundation.Resolver) (Registration, error) {
		if _, err := foundation.Resolve(resolver, registryRegistrationKey(key)); err != nil {
			return Registration{}, err
		}
		return item, nil
	})
}

// RegisterRegistry assembles contributed guards and policies through the same
// bounded immutable registry used by direct application construction.
func RegisterRegistry(r *foundation.Registrar, key foundation.Key[*Registry], config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := foundation.Provide(r, registryRegistrationKey(key), registryRegistration{}); err != nil {
		return err
	}
	return foundation.Factory(r, key, func(resolver foundation.Resolver) (*Registry, error) {
		registrations, err := foundation.Contributions(resolver, authorizationContributions(key))
		if err != nil {
			return nil, err
		}
		return NewRegistry(config, registrations...)
	})
}
