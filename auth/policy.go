package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/value"
)

type PolicyName string

// Policy retains both subject and resource types. It loads the subject through
// the supplied matching guard; callers cannot substitute an unauthenticated
// model. Decisions are evaluated each time, never cached as credential claims.
// Registered Before/After hooks for the same model run around the callback.
type Policy[M, R any] struct{ definition *policyDefinition[M, R] }
type policyDefinition[M, R any] struct {
	id       *declarationID
	name     PolicyName
	evaluate func(context.Context, M, R) (bool, error)
	guest    func(context.Context, value.Optional[M], R) (bool, error)
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
	decision, err := p.Inspect(ctx, guard, resource)
	if err != nil {
		return false, err
	}
	return decision.Allowed(), nil
}
func (p Policy[M, R]) Authorize(ctx context.Context, guard Guard[M], resource R) error {
	decision, err := p.Inspect(ctx, guard, resource)
	if err != nil {
		return err
	}
	return decision.Err()
}

// Inspect evaluates registered Before hooks, the policy and After vetoes for
// the current model, returning the decision and any typed Denial instead of
// only a boolean. Operational failures remain errors and never allow.
func (p Policy[M, R]) Inspect(ctx context.Context, guard Guard[M], resource R) (Decision, error) {
	if err := p.Validate(); err != nil {
		return Decision{}, err
	}
	scope, err := currentScope(ctx)
	if err != nil {
		return Decision{}, err
	}
	if err := p.ValidateIn(scope.registry); err != nil {
		return Decision{}, err
	}
	subject, err := guard.Require(ctx)
	if err != nil {
		return Decision{}, err
	}
	return decide(ctx, scope, p.definition.id, p.definition.name, subject, func(op context.Context) (bool, error) {
		return p.definition.evaluate(op, subject, resource)
	})
}

// decide is shared by policies and permissions: Before hooks may decide first,
// then the callback, then After hooks may veto any allow, including one a
// Before hook granted (a global kill-switch also stops super-administrators).
// A returned Denial is a typed deny; any other failure stays an error and never
// grants.
func decide[M any](ctx context.Context, scope *Scope, id *declarationID, name PolicyName, subject M, evaluate func(context.Context) (bool, error)) (Decision, error) {
	verdict, err := runHooks(ctx, scope, beforePhase, subject, name)
	if err != nil {
		return denied(err)
	}
	if verdict == Deny {
		return Decision{}, nil
	}
	if verdict != Allow {
		var allowed bool
		err = scope.evaluate(ctx, id, func(op context.Context) error {
			var err error
			allowed, err = evaluate(op)
			return err
		})
		if err != nil {
			return denied(err)
		}
		if !allowed {
			return Decision{}, nil
		}
	}
	verdict, err = runHooks(ctx, scope, afterPhase, subject, name)
	if err != nil {
		return denied(err)
	}
	return Decision{allowed: verdict != Deny}, nil
}
func denied(err error) (Decision, error) {
	// Scope.run already inspected extension errors while retaining ownership.
	denial, found := err.(*Denial)
	if found && denial != nil {
		return Decision{denial: denial}, nil
	}
	return Decision{}, err
}

// GuestPolicy evaluates a resource for an optional model: anonymous requests
// receive an omitted value instead of an Unauthenticated error. Invalid,
// revoked or pending credentials still fail. Before/After hooks apply only when
// a model is present. It shares the policy registry namespace.
type GuestPolicy[M, R any] struct{ policy Policy[M, R] }

func DefineGuestPolicy[M, R any](name PolicyName, evaluate func(context.Context, value.Optional[M], R) (bool, error)) GuestPolicy[M, R] {
	var check func(context.Context, M, R) (bool, error)
	if evaluate != nil {
		check = func(ctx context.Context, subject M, resource R) (bool, error) {
			return evaluate(ctx, value.Set(subject), resource)
		}
	}
	p := DefinePolicy(name, check)
	p.definition.guest = evaluate
	return GuestPolicy[M, R]{policy: p}
}
func (p GuestPolicy[M, R]) Name() PolicyName                    { return p.policy.Name() }
func (p GuestPolicy[M, R]) Validate() error                     { return p.policy.Validate() }
func (p GuestPolicy[M, R]) Registration() Registration          { return p.policy.Registration() }
func (p GuestPolicy[M, R]) ValidateIn(registry *Registry) error { return p.policy.ValidateIn(registry) }
func (p GuestPolicy[M, R]) Allows(ctx context.Context, guard Guard[M], resource R) (bool, error) {
	decision, err := p.Inspect(ctx, guard, resource)
	if err != nil {
		return false, err
	}
	return decision.Allowed(), nil
}
func (p GuestPolicy[M, R]) Authorize(ctx context.Context, guard Guard[M], resource R) error {
	decision, err := p.Inspect(ctx, guard, resource)
	if err != nil {
		return err
	}
	return decision.Err()
}
func (p GuestPolicy[M, R]) Inspect(ctx context.Context, guard Guard[M], resource R) (Decision, error) {
	if err := p.policy.Validate(); err != nil {
		return Decision{}, err
	}
	scope, err := currentScope(ctx)
	if err != nil {
		return Decision{}, err
	}
	if err := p.policy.ValidateIn(scope.registry); err != nil {
		return Decision{}, err
	}
	subject, err := guard.Optional(ctx)
	if err != nil {
		return Decision{}, err
	}
	if subject.IsSet() {
		return p.policy.Inspect(ctx, guard, resource)
	}
	definition := p.policy.definition
	var allowed bool
	err = scope.evaluate(ctx, definition.id, func(op context.Context) error {
		var err error
		allowed, err = definition.guest(op, value.Optional[M]{}, resource)
		return err
	})
	if err != nil {
		return denied(err)
	}
	return Decision{allowed: allowed}, nil
}
