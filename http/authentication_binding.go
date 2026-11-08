package http

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// authenticationBinding is shared by typed payload endpoints and native routes.
// It owns declaration validation, immutable requirements and scope attribution.
type authenticationBinding[M any] struct {
	guardRequirements[M]
	authentication   *Authentication
	guard            auth.Guard[M]
	alternatives     []GuardAlternative[M]
	actorMiddleware  []Middleware
	requirementError error
	ticket           auth.TicketRedeemer
}

// authenticationWithActorMiddleware appends middleware that runs after authentication and its
// scope/permission requirements, inside the authenticated request scope.
func authenticationWithActorMiddleware[M any](b authenticationBinding[M], middlewares ...Middleware) authenticationBinding[M] {
	if len(middlewares) == 0 {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Invalid, "HTTP actor middleware requires a declaration")
		}
		return b
	}
	b.actorMiddleware = append(slices.Clone(b.actorMiddleware), middlewares...)
	return b
}

// authenticationChain is the authentication middleware followed by the actor stage.
func authenticationChain[M any](b authenticationBinding[M], optional bool) []Middleware {
	return append([]Middleware{authenticationMiddleware(b, optional)}, b.actorMiddleware...)
}

func authenticationWithScopes[M any](b authenticationBinding[M], required auth.AccessScopes[M]) authenticationBinding[M] {
	if b.alternatives != nil {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guard alternatives cannot be mixed with flat requirements")
		}
		return b
	}
	if b.requiredScopes != nil {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Duplicate, "HTTP access scopes are already declared")
		}
		return b
	}
	b.requiredScopes = &required
	return b
}
func authenticationWithPermissions[M any](b authenticationBinding[M], required ...auth.Permission[M]) authenticationBinding[M] {
	if b.alternatives != nil {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guard alternatives cannot be mixed with flat requirements")
		}
		return b
	}
	if b.requiredPermissions != nil {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Duplicate, "HTTP permissions are already declared")
		}
		return b
	}
	if len(required) == 0 || len(required) > auth.MaxRegistrations {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Invalid, "HTTP permissions require a bounded nonempty declaration")
		}
		return b
	}
	b.requiredPermissions = slices.Clone(required)
	return b
}
func authenticationValidate[M any](b authenticationBinding[M], access Access, optional bool) error {
	if b.requirementError != nil {
		return b.requirementError
	}
	if b.requiredScopes != nil && (optional || b.requiredScopes.Len() == 0) {
		return safeerror.NewFault(fault.Invalid, "HTTP scope requirements need a nonempty authenticated declaration")
	}
	if err := b.authentication.validate(); err != nil {
		return err
	}
	if err := b.guard.ValidateIn(b.authentication.registry); err != nil {
		return err
	}
	if optional && len(b.requiredPermissions) > 0 {
		return safeerror.NewFault(fault.Invalid, "HTTP permission requirements need an authenticated endpoint")
	}
	validatePermissions := func(requirements guardRequirements[M]) error {
		for _, permission := range requirements.requiredPermissions {
			if err := permission.ValidateIn(b.authentication.registry); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validatePermissions(b.guardRequirements); err != nil {
		return err
	}
	for _, alternative := range b.alternatives {
		if err := validatePermissions(alternative.requirements); err != nil {
			return err
		}
	}
	if err := authenticationInfo(b, optional).ValidateRequirements(); err != nil {
		return err
	}
	if !b.authentication.hasSource(b.guard.Source()) {
		return safeerror.NewFault(fault.Missing, "HTTP authentication guard source is not configured")
	}
	expected := Guarded
	if optional {
		expected = Public
	}
	if access != expected {
		return safeerror.NewFault(fault.Invalid, "authentication adapter does not match route access")
	}
	return nil
}
func authenticationInfo[M any](b authenticationBinding[M], optional bool) *AuthenticationInfo {
	info := &AuthenticationInfo{Guard: b.guard.Name(), Provider: b.guard.ProviderName(), Optional: optional}
	for _, source := range b.authentication.sources {
		if source.name == b.guard.Source() {
			info.Credential = source.info
			break
		}
	}
	flat := b.guardRequirements.info()
	info.RequiredScopes, info.RequiredPermissions = flat.RequiredScopes, flat.RequiredPermissions
	if b.alternatives != nil {
		info.AnyOf = make([]GuardAlternativeInfo, len(b.alternatives))
		for i, alternative := range b.alternatives {
			info.AnyOf[i] = alternative.requirements.info()
		}
	}
	if b.ticket != nil {
		info.Credential = CredentialInfo{Source: b.guard.Source(), Kind: TicketCredentialKind, Name: ticketParameter}
	}
	return info
}
func authenticationMiddleware[M any](b authenticationBinding[M], optional bool) Middleware {
	resolve := func(ctx context.Context) (context.Context, error) {
		if b.alternatives != nil {
			return b.admitAlternative(ctx)
		}
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
	}
	if b.ticket != nil {
		return b.authentication.credentialsMiddleware(b.guard.Source(), resolve, ticketInputs(b.ticket), false)
	}
	return b.authentication.middleware(b.guard.Source(), resolve)
}
