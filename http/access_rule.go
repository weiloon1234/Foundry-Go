package http

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// AccessRule is the access decision of a guarded route. Only AllOf, AnyOf and
// GuardBinding.AnyActor produce one, and every constructor of a guarded route
// requires one, so a route cannot be declared without deciding who may call it.
//
// Example: http.Authenticated(endpoint, binding, http.AllOf(http.Scopes(read)))
type AccessRule[M any] interface {
	applyAccess(authenticationBinding[M]) authenticationBinding[M]
}

type anyActorRule[M any] struct{}

func (anyActorRule[M]) applyAccess(binding authenticationBinding[M]) authenticationBinding[M] {
	binding.accessDeclared = true
	return binding
}

// AnyActor is the access rule of a route open to every authenticated actor of
// this guard: no scope and no permission is required. It is the only spelling of
// that decision.
//
// Example: http.Authenticated(logoutEndpoint, admins, admins.AnyActor())
func (b GuardBinding[M]) AnyActor() AccessRule[M] { return anyActorRule[M]{} }

// AnyActorOf is AnyActor for RequireAuthentication and RequireRouteAuthentication,
// which take a guard instead of a binding: the route is open to every
// authenticated actor of guard.
//
// Example: http.RequireAuthentication(endpoint, authentication, guard, http.AnyActorOf(guard))
func AnyActorOf[M any](guard auth.Guard[M]) AccessRule[M] { return anyActorRule[M]{} }

func (b authenticationBinding[M]) withAccess(rule AccessRule[M]) authenticationBinding[M] {
	if rule == nil {
		if b.requirementError == nil {
			b.requirementError = safeerror.NewFault(fault.Invalid, "HTTP guarded route requires an access rule: http.AllOf, http.AnyOf or AnyActor")
		}
		return b
	}
	return rule.applyAccess(b)
}
