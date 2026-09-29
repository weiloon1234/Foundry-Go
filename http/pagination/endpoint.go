package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// NumberedEndpoint receives typed page input and expects an ORM page of explicit
// response DTOs. Handle supplies metadata and navigation before JSON encoding.
type NumberedEndpoint[P, F, T any] = pageEndpoint[P, F, query.PageRequest, query.Page[T], NumberedResponse[T]]

// SimpleEndpoint is the corresponding count-free transport contract.
type SimpleEndpoint[P, F, T any] = pageEndpoint[P, F, query.PageRequest, query.SimplePage[T], SimpleResponse[T]]

type pageEndpoint[P, F, W, Page, Response any] struct {
	transport      foundryhttp.Endpoint[P, parameters[F, W], foundryhttp.NoBody, Response]
	links          LinkMode
	validateWindow func(W) error
	classify       func(error) error
	finish         func(pageRequest[P, F, W], Page, func(W) (string, error)) (Response, error)
	err            error
}

// DefineNumbered composes generated filters, defaulted pagination and a declared
// item DTO. The endpoint accepts no request body; ordinary route/method rules
// apply. Config.Links=PublicLinks requires PublicURLs; ApplyMiddleware rejects a
// router whose PublicLinks routes are not covered by it.
func DefineNumbered[P, F, T any](route foundryhttp.Route[P], filters foundryhttp.Query[F], item contract.JSON[T], config Config) NumberedEndpoint[P, F, T] {
	return newOffsetEndpoint(route, filters, NumberedJSON(item), config, numberedResponse[P, F, T](config.navigation()))
}

// DefineSimple creates the same thin handler boundary without a count query or
// fabricated total metadata. Handlers return query.SimplePage[T].
func DefineSimple[P, F, T any](route foundryhttp.Route[P], filters foundryhttp.Query[F], item contract.JSON[T], config Config) SimpleEndpoint[P, F, T] {
	return newOffsetEndpoint(route, filters, SimpleJSON(item), config, simpleResponse[P, F, T](config.navigation()))
}
func newOffsetEndpoint[P, F, Page, Response any](route foundryhttp.Route[P], filters foundryhttp.Query[F], response contract.JSON[Response], config Config, finish func(Request[P, F], Page, func(query.PageRequest) (string, error)) (Response, error)) pageEndpoint[P, F, query.PageRequest, Page, Response] {
	if err := config.Validate(); err != nil {
		return pageEndpoint[P, F, query.PageRequest, Page, Response]{err: err}
	}
	maximumPage := config.maximumPage()
	validate := func(page query.PageRequest) error {
		if err := page.Validate(); err != nil {
			return err
		}
		if page.Size > config.MaximumSize {
			return fault.New(fault.Invalid, "page size exceeds endpoint maximum")
		}
		if page.Number > maximumPage {
			return errBeyondMaximumPage
		}
		return nil
	}
	return newPageEndpoint(route, pageQuery(filters, config), pageRules[F](config), response, config.Links, validate, finish)
}

// errBeyondMaximumPage lets navigation omit a link the endpoint would reject.
var errBeyondMaximumPage = fault.New(fault.Invalid, "page exceeds endpoint maximum depth")

func newPageEndpoint[P, F, W, Page, Response any](route foundryhttp.Route[P], parameters foundryhttp.Query[parameters[F, W]], rules validation.Rule[parameters[F, W]], response contract.JSON[Response], links LinkMode, validate func(W) error, finish func(pageRequest[P, F, W], Page, func(W) (string, error)) (Response, error)) pageEndpoint[P, F, W, Page, Response] {
	return pageEndpoint[P, F, W, Page, Response]{
		transport: foundryhttp.DefineEndpoint(route, parameters, foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, response)).WithQueryValidation(rules),
		links:     links, validateWindow: validate, finish: finish,
	}
}
func (e pageEndpoint[P, F, W, Page, Response]) Validate() error {
	if e.err != nil {
		return e.err
	}
	if e.finish == nil || e.validateWindow == nil || e.links != RelativeLinks && e.links != PublicLinks {
		return fault.New(fault.Invalid, "pagination endpoint is not defined")
	}
	return e.transport.Validate()
}
func (e pageEndpoint[P, F, W, Page, Response]) ID() foundryhttp.RouteID { return e.transport.ID() }
func (e pageEndpoint[P, F, W, Page, Response]) Method() foundryhttp.Method {
	return e.transport.Method()
}
func (e pageEndpoint[P, F, W, Page, Response]) Pattern() string { return e.transport.Pattern() }
func (e pageEndpoint[P, F, W, Page, Response]) Description() (foundryhttp.EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return e.transport.Description()
}
func (e pageEndpoint[P, F, W, Page, Response]) Within(scope foundryhttp.Scope) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.Within(scope)
	return e
}
func (e pageEndpoint[P, F, W, Page, Response]) WithMiddleware(middleware ...foundryhttp.Middleware) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.WithMiddleware(middleware...)
	return e
}
func (e pageEndpoint[P, F, W, Page, Response]) WithLimits(limits foundryhttp.EndpointLimits) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.WithLimits(limits)
	return e
}
func (e pageEndpoint[P, F, W, Page, Response]) WithErrors(declarations ...foundryhttp.ErrorDeclaration) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.WithErrors(declarations...)
	return e
}
func (e pageEndpoint[P, F, W, Page, Response]) WithPathValidation(rules ...validation.Rule[P]) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.WithPathValidation(rules...)
	return e
}

// WithFiltersValidation reuses generated field rules without adding a synthetic
// 'filters' prefix to actual query names or exported validation metadata.
func (e pageEndpoint[P, F, W, Page, Response]) WithFiltersValidation(rules ...validation.Rule[F]) pageEndpoint[P, F, W, Page, Response] {
	e.transport = e.transport.WithQueryValidation(validation.Embed(validation.All(rules...), func(input parameters[F, W]) F { return input.Filters }))
	return e
}

// URL builds a page URL from concrete route, filter and ORM request values. It
// uses the same query codecs/default declarations as the handler boundary and
// never derives an origin from an arbitrary incoming request Host.
func (e pageEndpoint[P, F, W, Page, Response]) URL(ctx context.Context, path P, filters F, page W) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if err := e.validateWindow(page); err != nil {
		return "", err
	}
	relative, err := e.transport.URL(ctx, path, parameters[F, W]{Filters: filters, Page: page})
	if err != nil {
		return "", err
	}
	if e.links == PublicLinks {
		return foundryhttp.PublicURL(ctx, relative)
	}
	return relative, nil
}

// Handle binds a domain page read. Input validation completes first; response
// metadata and links are completed before native JSON commits any success bytes.
func (e pageEndpoint[P, F, W, Page, Response]) Handle(handler func(context.Context, pageRequest[P, F, W]) (Page, error)) foundryhttp.RouteRegistration {
	if err := e.Validate(); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(fault.New(fault.Invalid, "pagination endpoint requires a handler"))
	}
	return e.withLinkRequirements().transport.Handle(func(ctx context.Context, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) (Response, error) {
		request := request(input)
		page, err := handler(ctx, request)
		return e.complete(ctx, request, page, err)
	})
}

// withLinkRequirements declares, innermost on the route, that PublicLinks
// generate absolute URLs. Router assembly then rejects a missing PublicURLs
// policy instead of failing each request with a 500.
func (e pageEndpoint[P, F, W, Page, Response]) withLinkRequirements() pageEndpoint[P, F, W, Page, Response] {
	if e.links == PublicLinks {
		e.transport = e.transport.WithMiddleware(foundryhttp.RequirePublicURLs())
	}
	return e
}

// complete is the shared post-handler boundary for public and authenticated pages.
func (e pageEndpoint[P, F, W, Page, Response]) complete(ctx context.Context, request pageRequest[P, F, W], page Page, err error) (Response, error) {
	if err != nil {
		if e.classify != nil {
			err = e.classify(err)
		}
		return *new(Response), err
	}
	if err := ctx.Err(); err != nil {
		return *new(Response), err
	}
	response, err := e.finish(request, page, func(target W) (string, error) {
		return e.URL(ctx, request.Path, request.Filters, target)
	})
	if err != nil {
		return *new(Response), foundryhttp.InternalError.WithCause(err)
	}
	return response, nil
}
