package http

import (
	"fmt"

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

// accessRuleMissing is the one text of the refusal of a guarded route declared
// without a rule; the located refusal prefixes it with the route.
const accessRuleMissing = "a guarded route requires an access rule: http.AllOf, http.AnyOf or binding.AnyActor()"

// withAccess records the rule on a fresh binding. A rule that is nil or reports its
// own declaration error is retained as accessError, so validation can name the
// route that declared it.
func (b authenticationBinding[M]) withAccess(rule AccessRule[M]) authenticationBinding[M] {
	if rule == nil {
		return b
	}
	b = rule.applyAccess(b)
	b.accessError, b.requirementError = b.requirementError, nil
	return b
}

// accessRefusal is the registration refusal of an absent or invalid access rule,
// located by the route's method, pattern and ID.
func (b authenticationBinding[M]) accessRefusal(method Method, pattern string, id RouteID) error {
	route := fmt.Sprintf("%s %s (%s)", method, pattern, id)
	if b.accessError != nil {
		return safeerror.Prefix(route, b.accessError)
	}
	return safeerror.Prefix(route, safeerror.NewFault(fault.Invalid, accessRuleMissing))
}
