package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"slices"
)

// authenticationBinding is shared by typed payload endpoints and native routes.
// It owns declaration validation, immutable requirements and scope attribution.
type authenticationBinding[M any] struct {
	authentication      *Authentication
	guard               auth.Guard[M]
	requiredScopes      *auth.AccessScopes[M]
	requiredPermissions []auth.Permission[M]
	actorMiddleware     []Middleware
	requirementError    error
}

// withActorMiddleware appends middleware that runs after authentication and its
// scope/permission requirements, inside the authenticated request scope.
func (b authenticationBinding[M]) withActorMiddleware(middlewares ...Middleware) authenticationBinding[M] {
	if len(middlewares) == 0 {
		b.requirementError = fault.New(fault.Invalid, "HTTP actor middleware requires a declaration")
		return b
	}
	b.actorMiddleware = append(slices.Clone(b.actorMiddleware), middlewares...)
	return b
}

// chain is the authentication middleware followed by the actor stage.
func (b authenticationBinding[M]) chain(optional bool) []Middleware {
	return append([]Middleware{b.middleware(optional)}, b.actorMiddleware...)
}

func (b authenticationBinding[M]) withScopes(required auth.AccessScopes[M]) authenticationBinding[M] {
	if b.requiredScopes != nil {
		b.requirementError = fault.New(fault.Duplicate, "HTTP access scopes are already declared")
		return b
	}
	b.requiredScopes = &required
	return b
}
func (b authenticationBinding[M]) withPermissions(required ...auth.Permission[M]) authenticationBinding[M] {
	if b.requiredPermissions != nil {
		b.requirementError = fault.New(fault.Duplicate, "HTTP permissions are already declared")
		return b
	}
	if len(required) == 0 || len(required) > auth.MaxRegistrations {
		b.requirementError = fault.New(fault.Invalid, "HTTP permissions require a bounded nonempty declaration")
		return b
	}
	b.requiredPermissions = slices.Clone(required)
	return b
}
func (b authenticationBinding[M]) validate(access Access, optional bool) error {
	if b.requirementError != nil {
		return b.requirementError
	}
	if b.requiredScopes != nil && (optional || b.requiredScopes.Len() == 0) {
		return fault.New(fault.Invalid, "HTTP scope requirements need a nonempty authenticated declaration")
	}
	if err := b.authentication.validate(); err != nil {
		return err
	}
	if err := b.guard.ValidateIn(b.authentication.registry); err != nil {
		return err
	}
	if optional && len(b.requiredPermissions) > 0 {
		return fault.New(fault.Invalid, "HTTP permission requirements need an authenticated endpoint")
	}
	names := make(map[auth.PermissionName]bool, len(b.requiredPermissions))
	for _, permission := range b.requiredPermissions {
		if err := permission.ValidateIn(b.authentication.registry); err != nil {
			return err
		}
		if names[permission.Name()] {
			return fault.New(fault.Duplicate, "HTTP permission requirement is repeated")
		}
		names[permission.Name()] = true
	}
	if !b.authentication.hasSource(b.guard.Source()) {
		return fault.New(fault.Missing, "HTTP authentication guard source is not configured")
	}
	expected := Guarded
	if optional {
		expected = Public
	}
	if access != expected {
		return fault.New(fault.Invalid, "authentication adapter does not match route access")
	}
	return nil
}
func (b authenticationBinding[M]) info(optional bool) *AuthenticationInfo {
	info := &AuthenticationInfo{Guard: b.guard.Name(), Provider: b.guard.ProviderName(), Optional: optional}
	for _, source := range b.authentication.sources {
		if source.name == b.guard.Source() {
			info.Credential = source.info
			break
		}
	}
	if b.requiredScopes != nil {
		info.RequiredScopes = b.requiredScopes.Names()
	}
	for _, permission := range b.requiredPermissions {
		info.RequiredPermissions = append(info.RequiredPermissions, permission.Name())
	}
	return info
}
func (b authenticationBinding[M]) middleware(optional bool) Middleware {
	return b.authentication.middleware(b.guard.Source(), func(ctx context.Context) (context.Context, error) {
		if optional {
			subject, err := b.guard.Optional(ctx)
			if err != nil {
				return nil, err
			}
			if !subject.IsSet() {
				// This selected guard is anonymous even when an outer caller
				// carried another model/system origin. Preserve only request data.
				origin, err := (attribution.Origin{}).WithRequest(attribution.FromContext(ctx).Request())
				if err != nil {
					return nil, err
				}
				return attribution.WithContext(ctx, origin)
			}
		}
		if b.requiredScopes != nil {
			if _, err := b.guard.RequireScopes(ctx, *b.requiredScopes); err != nil {
				return nil, err
			}
		}
		attributed, err := b.guard.WithAttribution(ctx)
		if err != nil {
			return nil, err
		}
		for _, permission := range b.requiredPermissions {
			if err := permission.Authorize(attributed, b.guard); err != nil {
				return nil, err
			}
		}
		return attributed, nil
	})
}
