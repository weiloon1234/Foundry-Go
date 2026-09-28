package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type PolicyName string

// Policy retains both subject and resource types. It loads the subject through
// the supplied matching guard; callers cannot substitute an unauthenticated
// model. Decisions are evaluated each time, never cached as credential claims.
type Policy[M, R any] struct{ definition *policyDefinition[M, R] }
type policyDefinition[M, R any] struct {
	id       *declarationID
	name     PolicyName
	evaluate func(context.Context, M, R) (bool, error)
}

func DefinePolicy[M, R any](name PolicyName, evaluate func(context.Context, M, R) (bool, error)) Policy[M, R] {
	return Policy[M, R]{definition: &policyDefinition[M, R]{id: &declarationID{}, name: name, evaluate: evaluate}}
}
func (p Policy[M, R]) Validate() error {
	if p.definition == nil || !identifier.Semantic(string(p.definition.name)) || p.definition.evaluate == nil {
		return fault.New(fault.Invalid, "authentication policy requires a semantic name and evaluator")
	}
	return nil
}
func (p Policy[M, R]) Name() PolicyName {
	if p.definition == nil {
		return ""
	}
	return p.definition.name
}
func (p Policy[M, R]) Registration() Registration {
	if p.definition == nil {
		return Registration{}
	}
	return Registration{kind: policyRegistration, id: p.definition.id, name: string(p.Name()), validate: p.Validate}
}

// ValidateIn checks exact declaration registration at assembly time without
// resolving a model or evaluating a decision. Matching names are insufficient.
func (p Policy[M, R]) ValidateIn(registry *Registry) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := registry.Validate(); err != nil {
		return err
	}
	if !registry.policies[p.definition.id] {
		return fault.New(fault.Missing, "authentication policy is not registered")
	}
	return nil
}

func (p Policy[M, R]) Allows(ctx context.Context, guard Guard[M], resource R) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	scope, err := currentScope(ctx)
	if err != nil {
		return false, err
	}
	if err := p.ValidateIn(scope.registry); err != nil {
		return false, err
	}
	subject, err := guard.Require(ctx)
	if err != nil {
		return false, err
	}
	var allowed bool
	err = scope.evaluate(ctx, p.definition.id, func(op context.Context) error {
		var err error
		allowed, err = p.definition.evaluate(op, subject, resource)
		return err
	})
	if err != nil {
		return false, err
	}
	return allowed, nil
}
func (p Policy[M, R]) Authorize(ctx context.Context, guard Guard[M], resource R) error {
	allowed, err := p.Allows(ctx, guard, resource)
	if err != nil {
		return err
	}
	if !allowed {
		return Forbidden
	}
	return nil
}
