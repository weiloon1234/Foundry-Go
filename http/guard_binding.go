package http

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// GuardBinding is a route group's concrete default actor. Separate model types
// require separate bindings; there is no runtime actor cast or global default.
type GuardBinding[M any] struct {
	authentication *Authentication
	guard          auth.Guard[M]
}

// BindGuard checks exact guard registration and credential-source availability in
// authentication.
func BindGuard[M any](authentication *Authentication, guard auth.Guard[M]) (GuardBinding[M], error) {
	if err := authentication.validate(); err != nil {
		return GuardBinding[M]{}, err
	}
	if err := guard.ValidateIn(authentication.registry); err != nil {
		return GuardBinding[M]{}, err
	}
	if !authentication.hasSource(guard.Source()) {
		return GuardBinding[M]{}, safeerror.NewFault(fault.Missing, "guard credential source is not configured")
	}
	return GuardBinding[M]{authentication, guard}, nil
}

// declaration is the authentication state every constructor of a guarded route starts from.
func (b GuardBinding[M]) declaration() authenticationBinding[M] {
	return authenticationBinding[M]{authentication: b.authentication, guard: b.guard}
}

// Select returns another binding for this model, leaving the group's default
// unchanged. A different actor model cannot be passed to this method.
func (b GuardBinding[M]) Select(guard auth.Guard[M]) (GuardBinding[M], error) {
	return BindGuard(b.authentication, guard)
}

// Guard returns the bound authentication guard.
func (b GuardBinding[M]) Guard() auth.Guard[M] { return b.guard }

// Authenticated binds a Guarded transport declaration to the bound guard's concrete
// subject and the access rule that says who may call endpoint: AllOf, AnyOf or
// binding.AnyActor(). Authentication runs before parameter/body decoding; its model
// is reused by the handler and policies within this request. Actor options run after
// the rule passes. A nil or zero rule cannot register.
//
// Example: http.Authenticated(profileEndpoint, users, http.AllOf(http.Scopes(read)))
func Authenticated[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], binding GuardBinding[M], rule AccessRule[M], options ...ActorOption[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	required := binding.declaration().withAccess(rule)
	return ConfigureAuthenticated(AuthenticatedEndpoint[P, Q, B, M, R]{endpoint: endpoint, binding: required}, options...)
}

// OptionallyAuthenticated adds the bound guard's optional concrete subject and applies
// actor options to endpoint.
func OptionallyAuthenticated[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], binding GuardBinding[M], options ...ActorOption[M]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	required := AuthenticatedEndpoint[P, Q, B, M, R]{endpoint: endpoint, binding: binding.declaration()}
	return ConfigureOptional(OptionalAuthenticationEndpoint[P, Q, B, M, R]{required: required}, options...)
}

// NewCookieAuthentication protects every selected cookie guard with the same
// origin policy, even under an API path. Use NewAuthentication with only bearer
// sources for a stateless API; browser-session adapters already own CSRF.
func NewCookieAuthentication(registry *auth.Registry, config CSRFConfig, sources ...CredentialSource) (*Authentication, error) {
	if len(sources) == 0 {
		return nil, safeerror.NewFault(fault.Invalid, "cookie authentication needs a source")
	}
	policy, err := compileCSRF(config)
	if err != nil {
		return nil, err
	}
	a, err := newAuthentication(registry, sources)
	if err != nil {
		return nil, err
	}
	for i := range a.sources {
		if a.sources[i].info.Kind != CookieCredentialKind {
			return nil, safeerror.NewFault(fault.Invalid, "cookie authentication requires only cookie sources")
		}
		a.sources[i].info.OriginProtection = true
	}
	a.csrf = &policy
	return a, nil
}
