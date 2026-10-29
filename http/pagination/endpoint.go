package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// NumberedEndpoint receives typed page input and expects an ORM page of explicit
// response DTOs. Handle supplies metadata and navigation before JSON encoding.
type NumberedEndpoint[P, F, T any] = pageEndpoint[P, F, orm.PageRequest, orm.PageResult[T], NumberedResponse[T]]

// SimpleEndpoint is the corresponding count-free transport contract.
type SimpleEndpoint[P, F, T any] = pageEndpoint[P, F, orm.PageRequest, orm.SimplePage[T], SimpleResponse[T]]

type pageCore struct {
	links LinkMode
	err   error
}

type pageEndpoint[P, F, W, Page, Response any] struct {
	pageCore
	transport      foundryhttp.Endpoint[P, parameters[F, W], foundryhttp.NoBody, Response]
	validateWindow func(W) error
	classify       func(error) error
	finish         func(pageRequest[P, F, W], Page, func(W) (string, error)) (Response, error)
}

// DefineNumbered composes generated filters, defaulted pagination and a declared
// item DTO. The endpoint accepts no request body; ordinary route/method rules
// apply. Config.Links=PublicLinks requires PublicURLs; ApplyMiddleware rejects a
// router whose PublicLinks routes are not covered by it.
func DefineNumbered[P, F, T any](route foundryhttp.Route[P], filters foundryhttp.Query[F], item contract.JSON[T], config Config, options ...foundryhttp.EndpointOption) NumberedEndpoint[P, F, T] {
	return Configure(newOffsetEndpoint(route, filters, NumberedJSON(item), config, numberedResponse[P, F, T](config.navigation())), options...)
}

// DefineSimple creates the same thin handler boundary without a count query or
// fabricated total metadata. Handlers return orm.SimplePage[T].
func DefineSimple[P, F, T any](route foundryhttp.Route[P], filters foundryhttp.Query[F], item contract.JSON[T], config Config, options ...foundryhttp.EndpointOption) SimpleEndpoint[P, F, T] {
	return Configure(newOffsetEndpoint(route, filters, SimpleJSON(item), config, simpleResponse[P, F, T](config.navigation())), options...)
}
func newOffsetEndpoint[P, F, Page, Response any](route foundryhttp.Route[P], filters foundryhttp.Query[F], response contract.JSON[Response], config Config, finish func(Request[P, F], Page, func(orm.PageRequest) (string, error)) (Response, error)) pageEndpoint[P, F, orm.PageRequest, Page, Response] {
	if err := config.Validate(); err != nil {
		return pageEndpoint[P, F, orm.PageRequest, Page, Response]{pageCore: pageCore{err: err}}
	}
	maximumPage := config.maximumPage()
	validate := func(page orm.PageRequest) error {
		if err := page.Validate(); err != nil {
			return err
		}
		if page.Size > config.MaximumSize {
			return safeerror.NewFault(fault.Invalid, "page size exceeds endpoint maximum")
		}
		if page.Number > maximumPage {
			return errBeyondMaximumPage
		}
		return nil
	}
	return newPageEndpoint(route, pageQuery(filters, config), pageRules[F](config), response, config.Links, validate, finish)
}

// errBeyondMaximumPage lets navigation omit a link the endpoint would reject.
var errBeyondMaximumPage = safeerror.NewFault(fault.Invalid, "page exceeds endpoint maximum depth")

func newPageEndpoint[P, F, W, Page, Response any](route foundryhttp.Route[P], parameters foundryhttp.Query[parameters[F, W]], rules validation.Rule[parameters[F, W]], response contract.JSON[Response], links LinkMode, validate func(W) error, finish func(pageRequest[P, F, W], Page, func(W) (string, error)) (Response, error)) pageEndpoint[P, F, W, Page, Response] {
	return pageEndpoint[P, F, W, Page, Response]{
		transport: foundryhttp.ValidateQuery(foundryhttp.DefineEndpoint(route, parameters, foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, response)), rules),
		pageCore:  pageCore{links: links}, validateWindow: validate, finish: finish,
	}
}
func Validate[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) error {
	if e.err != nil {
		return e.err
	}
	if e.finish == nil || e.validateWindow == nil || e.links != RelativeLinks && e.links != PublicLinks {
		return safeerror.NewFault(fault.Invalid, "pagination endpoint is not defined")
	}
	return foundryhttp.ValidateEndpoint(e.transport)
}
func EndpointID[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) foundryhttp.RouteID {
	return foundryhttp.EndpointID(e.transport)
}
func EndpointMethod[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) foundryhttp.Method {
	return foundryhttp.EndpointMethod(e.transport)
}
func EndpointPattern[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) string {
	return foundryhttp.EndpointPattern(e.transport)
}
func Inspect[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) (foundryhttp.EndpointInfo, error) {
	if err := Validate(e); err != nil {
		return foundryhttp.EndpointInfo{}, err
	}
	return foundryhttp.InspectEndpoint(e.transport)
}
func ValidatePath[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response], rules ...validation.Rule[P]) pageEndpoint[P, F, W, Page, Response] {
	e.transport = foundryhttp.ValidatePath(e.transport, rules...)
	return e
}

// ValidateFilters reuses generated field rules without adding a synthetic
// 'filters' prefix to actual query names or exported validation metadata.
func ValidateFilters[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response], rules ...validation.Rule[F]) pageEndpoint[P, F, W, Page, Response] {
	e.transport = foundryhttp.ValidateQuery(e.transport, validation.Embed(validation.All(rules...), func(input parameters[F, W]) F { return input.Filters }))
	return e
}

// URL builds a page URL from concrete route, filter and ORM request values. It
// uses the same query codecs/default declarations as the handler boundary and
// never derives an origin from an arbitrary incoming request Host.
func (e pageEndpoint[P, F, W, Page, Response]) URL(ctx context.Context, path P, filters F, page W) (string, error) {
	if err := Validate(e); err != nil {
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
	if err := Validate(e); err != nil {
		return foundryhttp.InvalidRouteRegistration(err)
	}
	if handler == nil {
		return foundryhttp.InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "pagination endpoint requires a handler"))
	}
	return pageEndpointWithLinkRequirements(e).transport.Handle(func(ctx context.Context, input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) (Response, error) {
		request := request(input)
		page, err := handler(ctx, request)
		return pageEndpointComplete(e, ctx, request, page, err)
	})
}

// withLinkRequirements declares, innermost on the route, that PublicLinks
// generate absolute URLs. Router assembly then rejects a missing PublicURLs
// policy instead of failing each request with a 500.
func pageEndpointWithLinkRequirements[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response]) pageEndpoint[P, F, W, Page, Response] {
	if e.links == PublicLinks {
		e.transport = foundryhttp.ConfigureEndpoint(e.transport, foundryhttp.Use(foundryhttp.RequirePublicURLs()))
	}
	return e
}

// complete is the shared post-handler boundary for public and authenticated pages.
func pageEndpointComplete[P, F, W, Page, Response any](e pageEndpoint[P, F, W, Page, Response], ctx context.Context, request pageRequest[P, F, W], page Page, err error) (Response, error) {
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
