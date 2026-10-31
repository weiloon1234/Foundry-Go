package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// EndpointOption configures a declaration without erasing its request or response types.
type EndpointOption interface{ endpointOption() endpointOptionValue }

// RouteOption is accepted by both route and endpoint constructors.
type RouteOption interface {
	EndpointOption
	routeOption()
}

type optionKind uint8

const (
	optionWithin optionKind = iota
	optionTimeout
	optionBodyLimit
	optionUse
	optionDocumentation
	optionLimits
	optionErrors
	optionTableOperation
)

type endpointOptionValue struct {
	tableOperation contract.TableOperationBinding
	kind           optionKind
	scope          Scope
	timeout        time.Duration
	bytes          int64
	middleware     []Middleware
	documentation  RouteDocumentation
	limits         EndpointLimits
	errors         []ErrorDeclaration
}
type routeOptionValue struct{ endpointOptionValue }

func (o endpointOptionValue) endpointOption() endpointOptionValue { return o }
func (routeOptionValue) routeOption()                             {}

func Within(scope Scope) RouteOption {
	scope.middlewares = slices.Clone(scope.middlewares)
	return routeOptionValue{endpointOptionValue{kind: optionWithin, scope: scope}}
}
func Timeout(timeout time.Duration) RouteOption {
	return routeOptionValue{endpointOptionValue{kind: optionTimeout, timeout: timeout}}
}
func BodyLimit(bytes int64) RouteOption {
	return routeOptionValue{endpointOptionValue{kind: optionBodyLimit, bytes: bytes}}
}
func Use(middleware ...Middleware) RouteOption {
	return routeOptionValue{endpointOptionValue{kind: optionUse, middleware: slices.Clone(middleware)}}
}
func Documentation(documentation RouteDocumentation) RouteOption {
	return routeOptionValue{endpointOptionValue{kind: optionDocumentation, documentation: *documentation.clone()}}
}
func Limits(limits EndpointLimits) EndpointOption {
	return endpointOptionValue{kind: optionLimits, limits: limits}
}
func Errors(declarations ...ErrorDeclaration) EndpointOption {
	return endpointOptionValue{kind: optionErrors, errors: slices.Clone(declarations)}
}

func configureRouteOption[P any](route Route[P], option endpointOptionValue) Route[P] {
	switch option.kind {
	case optionWithin:
		return routeWithin(route, option.scope)
	case optionTimeout:
		return routeWithTimeout(route, option.timeout)
	case optionBodyLimit:
		return routeWithBodyLimit(route, option.bytes)
	case optionUse:
		return routeWithMiddleware(route, option.middleware...)
	case optionDocumentation:
		return routeWithDocumentation(route, option.documentation)
	}
	return route
}
func ConfigureRoute[P any](route Route[P], options ...RouteOption) Route[P] {
	for _, option := range options {
		if option == nil {
			if route.err == nil {
				route.err = safeerror.NewFault(fault.Invalid, "route option is missing")
			}
			continue
		}
		route = configureRouteOption(route, option.endpointOption())
	}
	return route
}
func ConfigureEndpoint[P, Q, B, R any](endpoint Endpoint[P, Q, B, R], options ...EndpointOption) Endpoint[P, Q, B, R] {
	for _, option := range options {
		if option == nil {
			if endpoint.route.err == nil {
				endpoint.route.err = safeerror.NewFault(fault.Invalid, "endpoint option is missing")
			}
			continue
		}
		setting := option.endpointOption()
		switch setting.kind {
		case optionTableOperation:
			if endpoint.hasTableOperation {
				if endpoint.route.err == nil {
					endpoint.route.err = safeerror.NewFault(fault.Invalid, "table operation is declared more than once")
				}
			} else {
				endpoint.tableOperation, endpoint.hasTableOperation = setting.tableOperation, true
				binding := setting.tableOperation
				validKind := binding.Kind == contract.TableQuery || binding.Kind == contract.TableTicketExport || binding.Kind == contract.TableFileExport || binding.Kind == contract.TableDownload
				if (!identifier.Semantic(string(binding.Table)) || !validKind || (binding.Kind != contract.TableTicketExport && binding.RequestField != "") || (binding.Kind == contract.TableTicketExport && binding.RequestField == "")) && endpoint.route.err == nil {
					endpoint.route.err = safeerror.NewFault(fault.Invalid, "invalid table operation declaration")
				}
			}
		case optionLimits:
			endpoint.limits = setting.limits
		case optionErrors:
			endpoint.errors = append(slices.Clone(endpoint.errors), setting.errors...)
		default:
			endpoint.route = configureRouteOption(endpoint.route, setting)
		}
	}
	return endpoint
}

// GuardOption preserves ownership of credential scopes and permissions.
type GuardOption[M any] interface {
	applyGuard(authenticationBinding[M]) authenticationBinding[M]
}

// ActorOption is the middleware-only subset accepted by optional authentication.
type ActorOption[M any] interface {
	GuardOption[M]
	actorOption()
}
type guardOption[M any] struct {
	apply func(authenticationBinding[M]) authenticationBinding[M]
}

func (o guardOption[M]) applyGuard(binding authenticationBinding[M]) authenticationBinding[M] {
	return o.apply(binding)
}

type actorOption[M any] struct{ guardOption[M] }

func (actorOption[M]) actorOption() {}
func Scopes[M any](required auth.AccessScopes[M]) GuardOption[M] {
	return guardOption[M]{apply: func(binding authenticationBinding[M]) authenticationBinding[M] {
		return authenticationWithScopes(binding, required)
	}}
}
func Permissions[M any](required ...auth.Permission[M]) GuardOption[M] {
	required = slices.Clone(required)
	return guardOption[M]{apply: func(binding authenticationBinding[M]) authenticationBinding[M] {
		return authenticationWithPermissions(binding, required...)
	}}
}
func ActorMiddleware[M any](middleware ...Middleware) ActorOption[M] {
	middleware = slices.Clone(middleware)
	return actorOption[M]{guardOption[M]{apply: func(binding authenticationBinding[M]) authenticationBinding[M] {
		return authenticationWithActorMiddleware(binding, middleware...)
	}}}
}
func ConfigureAuthenticated[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R], options ...GuardOption[M]) AuthenticatedEndpoint[P, Q, B, M, R] {
	for _, option := range options {
		if option == nil {
			if endpoint.binding.requirementError == nil {
				endpoint.binding.requirementError = safeerror.NewFault(fault.Invalid, "authentication option is missing")
			}
		} else {
			endpoint.binding = option.applyGuard(endpoint.binding)
		}
	}
	return endpoint
}
func ConfigureOptional[P, Q, B, M, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, M, R], options ...ActorOption[M]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	for _, option := range options {
		if option == nil {
			if endpoint.required.binding.requirementError == nil {
				endpoint.required.binding.requirementError = safeerror.NewFault(fault.Invalid, "authentication option is missing")
			}
		} else {
			endpoint.required.binding = option.applyGuard(endpoint.required.binding)
		}
	}
	return endpoint
}

func ConfigureAuthenticatedRoute[P, M any](route AuthenticatedRoute[P, M], options ...GuardOption[M]) AuthenticatedRoute[P, M] {
	for _, option := range options {
		if option == nil {
			if route.binding.requirementError == nil {
				route.binding.requirementError = safeerror.NewFault(fault.Invalid, "authentication option is missing")
			}
		} else {
			route.binding = option.applyGuard(route.binding)
		}
	}
	return route
}
func ConfigureOptionalRoute[P, M any](route OptionalAuthenticationRoute[P, M], options ...ActorOption[M]) OptionalAuthenticationRoute[P, M] {
	for _, option := range options {
		if option == nil {
			if route.required.binding.requirementError == nil {
				route.required.binding.requirementError = safeerror.NewFault(fault.Invalid, "authentication option is missing")
			}
		} else {
			route.required.binding = option.applyGuard(route.required.binding)
		}
	}
	return route
}

type SignedOption struct {
	permanent bool
	ignored   []string
	ignore    bool
}

func PermanentLinks() SignedOption { return SignedOption{permanent: true} }
func IgnoredParameters(names ...string) SignedOption {
	return SignedOption{ignored: slices.Clone(names), ignore: true}
}
func ConfigureSigned[P, Q, B, R any](endpoint SignedEndpoint[P, Q, B, R], options ...SignedOption) SignedEndpoint[P, Q, B, R] {
	for _, option := range options {
		if option.permanent {
			endpoint = signedEndpointWithPermanentLinks(endpoint)
		}
		if option.ignore {
			endpoint = signedEndpointWithIgnoredParameters(endpoint, option.ignored...)
		}
	}
	return endpoint
}
func ConfigureSignedAuthenticated[P, Q, B, S, R any](endpoint SignedAuthenticatedEndpoint[P, Q, B, S, R], options ...SignedOption) SignedAuthenticatedEndpoint[P, Q, B, S, R] {
	for _, option := range options {
		if option.permanent {
			endpoint = signedAuthenticatedEndpointWithPermanentLinks(endpoint)
		}
		if option.ignore {
			endpoint = signedAuthenticatedEndpointWithIgnoredParameters(endpoint, option.ignored...)
		}
	}
	return endpoint
}
func ConfigureSignedRoute[P any](route SignedRoute[P], options ...SignedOption) SignedRoute[P] {
	for _, option := range options {
		if option.permanent {
			route = signedRouteWithPermanentLinks(route)
		}
		if option.ignore {
			route = signedRouteWithIgnoredParameters(route, option.ignored...)
		}
	}
	return route
}
func ConfigureSignedAuthenticatedRoute[P, S any](route SignedAuthenticatedRoute[P, S], options ...SignedOption) SignedAuthenticatedRoute[P, S] {
	for _, option := range options {
		if option.permanent {
			route = signedAuthenticatedRouteWithPermanentLinks(route)
		}
		if option.ignore {
			route = signedAuthenticatedRouteWithIgnoredParameters(route, option.ignored...)
		}
	}
	return route
}

func AuthenticatedEndpointID[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R]) RouteID {
	return EndpointID(endpoint.endpoint)
}
func AuthenticatedEndpointMethod[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R]) Method {
	return EndpointMethod(endpoint.endpoint)
}
func AuthenticatedEndpointPattern[P, Q, B, M, R any](endpoint AuthenticatedEndpoint[P, Q, B, M, R]) string {
	return EndpointPattern(endpoint.endpoint)
}
func OptionalEndpointID[P, Q, B, M, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, M, R]) RouteID {
	return EndpointID(endpoint.required.endpoint)
}
func OptionalEndpointMethod[P, Q, B, M, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, M, R]) Method {
	return EndpointMethod(endpoint.required.endpoint)
}
func OptionalEndpointPattern[P, Q, B, M, R any](endpoint OptionalAuthenticationEndpoint[P, Q, B, M, R]) string {
	return EndpointPattern(endpoint.required.endpoint)
}
