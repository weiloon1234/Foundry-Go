package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// AuthenticatedNumberedEndpoint supplies a concrete actor before a numbered page read.
type AuthenticatedNumberedEndpoint[P, F, A, T any] = authenticatedPageEndpoint[P, F, orm.PageRequest, A, orm.PageResult[T], NumberedResponse[T]]

// AuthenticatedSimpleEndpoint is the count-free authenticated page contract.
type AuthenticatedSimpleEndpoint[P, F, A, T any] = authenticatedPageEndpoint[P, F, orm.PageRequest, A, orm.SimplePage[T], SimpleResponse[T]]

// AuthenticatedCursorEndpoint keeps cursor owner M, actor A and public DTO T distinct.
type AuthenticatedCursorEndpoint[P, F, M, A, T any] = authenticatedPageEndpoint[P, F, orm.CursorRequest[M], A, CursorResult[M, T], CursorResponse[T]]

type authenticatedPageEndpoint[P, F, W, A, Page, Response any] struct {
	page      pageEndpoint[P, F, W, Page, Response]
	transport foundryhttp.AuthenticatedEndpoint[P, parameters[F, W], foundryhttp.NoBody, A, Response]
}

// Authenticated binds a Guarded numbered, simple or cursor endpoint to its concrete
// actor. Configure scope, middleware, limits, errors and validation on the page
// before binding. Authentication and authority retain the ordinary HTTP lifecycle.
func Authenticated[P, F, W, A, Page, Response any](endpoint pageEndpoint[P, F, W, Page, Response], binding foundryhttp.GuardBinding[A], options ...foundryhttp.GuardOption[A]) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	return authenticatedPageEndpoint[P, F, W, A, Page, Response]{
		page:      endpoint,
		transport: foundryhttp.Authenticated(pageEndpointWithLinkRequirements(endpoint).transport, binding, options...),
	}
}

// AuthorizeActor receives the actor and validated page request before the read.
// It uses HTTP request-hook isolation and error mapping, including nil validation.
func AuthorizeActor[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response], authorize func(context.Context, A, pageRequest[P, F, W]) error) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	var hook func(context.Context, A, foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) error
	if authorize != nil {
		hook = func(ctx context.Context, actor A, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) error {
			return authorize(ctx, actor, request(input))
		}
	}
	e.transport = foundryhttp.AuthorizeActor(e.transport, hook)
	return e
}

func ValidateAuthenticated[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response]) error {
	if err := Validate(e.page); err != nil {
		return err
	}
	return foundryhttp.ValidateAuthenticated(e.transport)
}
func AuthenticatedEndpointID[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response]) foundryhttp.RouteID {
	return EndpointID(e.page)
}
func AuthenticatedEndpointMethod[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response]) foundryhttp.Method {
	return EndpointMethod(e.page)
}
func AuthenticatedEndpointPattern[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response]) string {
	return EndpointPattern(e.page)
}
func InspectAuthenticated[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response]) (foundryhttp.EndpointInfo, error) {
	if err := ValidateAuthenticated(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectAuthenticated(e.transport)
}

// AuthenticatedURL validates the guarded endpoint and builds a URL from concrete
// route, filter and pagination values using the public adapter's link policy.
func AuthenticatedURL[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response], ctx context.Context, path P, filters F, page W) (string, error) {
	if err := ValidateAuthenticated(e); err != nil {
		return "", err
	}
	return URL(e.page, ctx, path, filters, page)
}

// HandleAuthenticated preserves actor, request and ORM page types through the
// ordinary guarded HTTP handler. Page validation, navigation and response completion are shared
// with the public adapter; authority is never inferred from a cursor token.
func HandleAuthenticated[P, F, W, A, Page, Response any](e authenticatedPageEndpoint[P, F, W, A, Page, Response], handler func(context.Context, A, pageRequest[P, F, W]) (Page, error)) foundryhttp.RouteRegistration {
	if err := ValidateAuthenticated(e); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "authenticated pagination endpoint requires a handler"))
	}
	return e.transport.Handle(func(ctx context.Context, actor A, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) (Response, error) {
		in := request(input)
		page, err := handler(ctx, actor, in)
		return pageEndpointComplete(e.page, ctx, in, page, err)
	})
}
