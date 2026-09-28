package http

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// GuardBinding is a route group's concrete default actor. Separate model types
// require separate bindings; there is no runtime actor cast or global default.
type GuardBinding[M any] struct {
	authentication *Authentication
	guard          auth.Guard[M]
}

func BindGuard[M any](authentication *Authentication, guard auth.Guard[M]) (GuardBinding[M], error) {
	if err := authentication.validate(); err != nil {
		return GuardBinding[M]{}, err
	}
	if err := guard.ValidateIn(authentication.registry); err != nil {
		return GuardBinding[M]{}, err
	}
	if !authentication.hasSource(guard.Source()) {
		return GuardBinding[M]{}, fault.New(fault.Missing, "guard credential source is not configured")
	}
	return GuardBinding[M]{authentication, guard}, nil
}

// Select returns another binding for this model, leaving the group's default
// unchanged. A different actor model cannot be passed to this method.
func (b GuardBinding[M]) Select(guard auth.Guard[M]) (GuardBinding[M], error) {
	return BindGuard(b.authentication, guard)
}
func (b GuardBinding[M]) Guard() auth.Guard[M] { return b.guard }
func Authenticated[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], binding GuardBinding[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	return RequireAuthentication(endpoint, binding.authentication, binding.guard)
}
func OptionallyAuthenticated[P, Q, B, M, R any](endpoint Endpoint[P, Q, B, R], binding GuardBinding[M]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	return OptionalAuthentication(endpoint, binding.authentication, binding.guard)
}

// NewCookieAuthentication protects every selected cookie guard with the same
// origin policy, even under an API path. Use NewAuthentication with only bearer
// sources for a stateless API; browser-session adapters already own CSRF.
func NewCookieAuthentication(registry *auth.Registry, config CSRFConfig, sources ...CredentialSource) (*Authentication, error) {
	if len(sources) == 0 {
		return nil, fault.New(fault.Invalid, "cookie authentication needs a source")
	}
	policy, err := compileCSRF(config)
	if err != nil {
		return nil, err
	}
	a, err := NewAuthentication(registry, sources...)
	if err != nil {
		return nil, err
	}
	for i := range a.sources {
		if a.sources[i].info.Kind != CookieCredentialKind {
			return nil, fault.New(fault.Invalid, "cookie authentication requires only cookie sources")
		}
		a.sources[i].info.OriginProtection = true
	}
	a.csrf = &policy
	return a, nil
}
