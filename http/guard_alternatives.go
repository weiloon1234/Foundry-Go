package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// MaxGuardAlternatives bounds an AnyOf declaration. Total scope and permission
// references are also bounded by auth.MaxAccessScopes and auth.MaxRegistrations.
const MaxGuardAlternatives = 32

type guardRequirements[M any] struct {
	requiredScopes      *auth.AccessScopes[M]
	requiredPermissions []auth.Permission[M]
}

func (r guardRequirements[M]) info() GuardAlternativeInfo {
	var info GuardAlternativeInfo
	if r.requiredScopes != nil {
		// Preserve an explicitly empty Scopes option for declaration validation.
		info.RequiredScopes = append([]auth.AccessScopeName{}, r.requiredScopes.Names()...)
	}
	for _, permission := range r.requiredPermissions {
		info.RequiredPermissions = append(info.RequiredPermissions, permission.Name())
	}
	return info
}

// GuardAlternative is an immutable conjunction of scopes and permissions for M.
// Construct it with AllOf. Its zero value is invalid and cannot grant access.
type GuardAlternative[M any] struct {
	requirements guardRequirements[M]
	err          error
}

// AllOf declares one nonempty alternative. Every scope and permission must pass.
// It retains declaration errors for endpoint/route validation before registration.
func AllOf[M any](requirements ...RequirementOption[M]) GuardAlternative[M] {
	var binding authenticationBinding[M]
	for _, option := range requirements {
		if option == nil {
			if binding.requirementError == nil {
				binding.requirementError = safeerror.NewFault(fault.Invalid, "guard requirement option is missing")
			}
		} else {
			binding = option.applyGuard(binding)
		}
	}
	err := binding.requirementError
	if err == nil {
		err = binding.guardRequirements.info().validate(true)
	}
	return GuardAlternative[M]{requirements: binding.guardRequirements, err: err}
}

// AnyOf requires one complete alternative to pass, in declaration order.
// An observed authority error stops admission; only a normal denial tries the
// next branch. It cannot be mixed with flat Scopes/Permissions or another AnyOf.
func AnyOf[M any](alternatives ...GuardAlternative[M]) GuardOption[M] {
	owned := append([]GuardAlternative[M]{}, alternatives...)
	return guardOption[M]{apply: func(binding authenticationBinding[M]) authenticationBinding[M] {
		if binding.requirementError != nil {
			return binding
		}
		if binding.alternatives != nil {
			binding.requirementError = safeerror.NewFault(fault.Duplicate, "HTTP guard alternatives are already declared")
			return binding
		}
		if binding.requiredScopes != nil || binding.requiredPermissions != nil {
			binding.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guard alternatives cannot be mixed with flat requirements")
			return binding
		}
		binding.alternatives = owned
		if len(owned) == 0 || len(owned) > MaxGuardAlternatives {
			binding.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guard alternatives require a bounded nonempty declaration")
		}
		for _, alternative := range owned {
			if binding.requirementError == nil {
				binding.requirementError = alternative.err
			}
		}
		return binding
	}}
}

func (b authenticationBinding[M]) admitAlternative(ctx context.Context) (context.Context, error) {
	if _, err := b.guard.Require(ctx); err != nil {
		return nil, err
	}
	var attributed context.Context
alternatives:
	for _, alternative := range b.alternatives {
		r := alternative.requirements
		if r.requiredScopes != nil {
			allowed, err := b.guard.AllowsScopes(ctx, *r.requiredScopes)
			if err != nil {
				return nil, err
			}
			if !allowed {
				continue
			}
		}
		if attributed == nil {
			var err error
			attributed, err = b.guard.WithAttribution(ctx)
			if err != nil {
				return nil, err
			}
		}
		for _, permission := range r.requiredPermissions {
			decision, err := permission.Inspect(attributed, b.guard)
			if err != nil {
				return nil, err
			}
			if !decision.Allowed() {
				continue alternatives
			}
		}
		return attributed, nil
	}
	return nil, auth.Forbidden.WithCause(nil)
}
