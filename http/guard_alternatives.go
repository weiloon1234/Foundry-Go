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
// Construct it with AllOf; it is an AccessRule and one branch of AnyOf. Its zero
// value is invalid and cannot grant access.
type GuardAlternative[M any] struct {
	requirements guardRequirements[M]
	err          error
}

// AllOf declares one nonempty access rule. Every scope and permission must pass.
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
// next branch. It is a complete AccessRule, so it cannot be mixed with another
// requirement.
func AnyOf[M any](alternatives ...GuardAlternative[M]) AccessRule[M] {
	return anyOfRule[M]{alternatives: append([]GuardAlternative[M]{}, alternatives...)}
}

type anyOfRule[M any] struct{ alternatives []GuardAlternative[M] }

func (r anyOfRule[M]) applyAccess(binding authenticationBinding[M]) authenticationBinding[M] {
	binding.accessDeclared = true
	binding.alternatives = r.alternatives
	if len(r.alternatives) == 0 || len(r.alternatives) > MaxGuardAlternatives {
		binding.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guard alternatives require a bounded nonempty declaration")
	}
	for _, alternative := range r.alternatives {
		if binding.requirementError == nil {
			binding.requirementError = alternative.err
		}
	}
	return binding
}

func (a GuardAlternative[M]) applyAccess(binding authenticationBinding[M]) authenticationBinding[M] {
	binding.accessDeclared = true
	binding.guardRequirements = a.requirements
	if binding.requirementError == nil {
		binding.requirementError = a.err
	}
	if binding.requirementError == nil && a.requirements.requiredScopes == nil && a.requirements.requiredPermissions == nil {
		binding.requirementError = safeerror.NewFault(fault.Invalid, "HTTP access rule was not built by AllOf")
	}
	return binding
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
