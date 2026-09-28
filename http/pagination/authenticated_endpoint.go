package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// AuthenticatedNumberedEndpoint supplies a concrete actor before a numbered page read.
type AuthenticatedNumberedEndpoint[P, F, A, T any] = authenticatedPageEndpoint[P, F, query.PageRequest, A, query.Page[T], NumberedResponse[T]]

// AuthenticatedSimpleEndpoint is the count-free authenticated page contract.
type AuthenticatedSimpleEndpoint[P, F, A, T any] = authenticatedPageEndpoint[P, F, query.PageRequest, A, query.SimplePage[T], SimpleResponse[T]]

// AuthenticatedCursorEndpoint keeps cursor owner M, actor A and public DTO T distinct.
type AuthenticatedCursorEndpoint[P, F, M, A, T any] = authenticatedPageEndpoint[P, F, query.CursorRequest[M], A, CursorResult[M, T], CursorResponse[T]]

type authenticatedPageEndpoint[P, F, W, A, Page, Response any] struct {
	page      pageEndpoint[P, F, W, Page, Response]
	transport foundryhttp.AuthenticatedEndpoint[P, parameters[F, W], foundryhttp.NoBody, A, Response]
}

// Authenticated binds a Guarded numbered, simple or cursor endpoint to its concrete
// actor. Configure scope, middleware, limits, errors and validation on the page
// before binding. Authentication and authority retain the ordinary HTTP lifecycle.
func Authenticated[P, F, W, A, Page, Response any](endpoint pageEndpoint[P, F, W, Page, Response], binding foundryhttp.GuardBinding[A]) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	return authenticatedPageEndpoint[P, F, W, A, Page, Response]{
		page:      endpoint,
		transport: foundryhttp.Authenticated(endpoint.transport, binding),
	}
}

// WithScopes requires the credential ceiling before input decoding.
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) WithScopes(required auth.AccessScopes[A]) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	e.transport = e.transport.WithScopes(required)
	return e
}

// WithPermissions requires current-model capabilities before input decoding.
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) WithPermissions(required ...auth.Permission[A]) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	e.transport = e.transport.WithPermissions(required...)
	return e
}

// WithAuthorization receives the actor and validated page request before the read.
// It uses HTTP request-hook isolation and error mapping, including nil validation.
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) WithAuthorization(authorize func(context.Context, A, pageRequest[P, F, W]) error) authenticatedPageEndpoint[P, F, W, A, Page, Response] {
	var hook func(context.Context, A, foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) error
	if authorize != nil {
		hook = func(ctx context.Context, actor A, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) error {
			return authorize(ctx, actor, request(input))
		}
	}
	e.transport = e.transport.WithAuthorization(hook)
	return e
}

func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) Validate() error {
	if err := e.page.Validate(); err != nil {
		return err
	}
	return e.transport.Validate()
}
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) ID() foundryhttp.RouteID {
	return e.page.ID()
}
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) Method() foundryhttp.Method {
	return e.page.Method()
}
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) Pattern() string {
	return e.page.Pattern()
}
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) URL(ctx context.Context, path P, filters F, page W) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.page.URL(ctx, path, filters, page)
}

// Handle preserves actor, request and ORM page types through the ordinary guarded
// HTTP handler. Page validation, navigation and response completion are shared
// with the public adapter; authority is never inferred from a cursor token.
func (e authenticatedPageEndpoint[P, F, W, A, Page, Response]) Handle(handler func(context.Context, A, pageRequest[P, F, W]) (Page, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "authenticated pagination endpoint requires a handler"))
	}
	return e.transport.Handle(func(ctx context.Context, actor A, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) (Response, error) {
		in := request(input)
		page, err := handler(ctx, actor, in)
		return e.page.complete(ctx, in, page, err)
	})
}
