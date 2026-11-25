package httpendpoints

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin flat-routes-6
// SavePageRequest carries the title submitted by an editor.
//
//foundry:dto http=body
type SavePageRequest struct {
	Title string `json:"title"`
}

// PageResource is the public representation returned after saving.
//
//foundry:dto role=response http=response
type PageResource struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// doc-example:end flat-routes-6

// doc-example:begin page-contracts
// Admin is the authenticated editor passed to page handlers.
type Admin struct{ ID int64 }
type pageRequest = http.Input[http.NoPath, http.NoQuery, SavePageRequest]
type pageService interface {
	Create(context.Context, Admin, pageRequest) (PageResource, error)
	Show(context.Context, pageRequest) (PageResource, error)
}

// PageProtected is the safe public failure for edits to protected pages.
var PageProtected = http.DefineError("page.protected", 409, "The page is protected.")

func preparePage(_ context.Context, in pageRequest) (http.NoQuery, SavePageRequest, error) {
	in.Body.Title = strings.TrimSpace(in.Body.Title)
	return in.Query, in.Body, nil
}

// doc-example:end page-contracts

// doc-example:begin flat-routes-1
func registerPage(adminGuard http.GuardBinding[Admin], manage auth.AccessScopes[Admin], service pageService) http.RouteRegistration {
	spec := http.RouteSpec{ID: "pages.create", Method: http.POST, Access: http.Guarded}
	path := http.StaticPath("/pages")
	adminScope := http.DefineScope("/admin", "admin")
	query, body, response := http.EmptyQuery(), SavePageBody(), PageResponse(201)
	pageRules := validation.Rules(SavePageRequestValidationFields().Title, validation.NonBlank[string]())
	createPage := service.Create

	route := http.DefineRoute(spec, path)
	route = http.ConfigureRoute(route, http.Within(adminScope))
	endpoint := http.DefineEndpoint(route, query, body, response)
	endpoint = http.ConfigureEndpoint(endpoint, http.Errors(PageProtected))
	endpoint = http.PrepareEndpoint(endpoint, preparePage)
	endpoint = http.ValidateBody(endpoint, pageRules)
	admin := http.Authenticated(endpoint, adminGuard, http.Scopes(manage))
	registration := admin.Handle(createPage)
	return registration
}

// doc-example:end flat-routes-1
// doc-example:begin flat-routes-3
func replayablePage(route http.Route[http.NoPath], access http.GuardBinding[Admin], store *idempotency.Store, definition idempotency.Definition) http.AuthenticatedIdempotentEndpoint[http.NoPath, http.NoQuery, SavePageRequest, Admin, PageResource] {
	query, body, response := http.EmptyQuery(), SavePageBody(), PageResponse(201)

	endpoint := http.DefineEndpoint(route, query, body, response)
	secured := http.Authenticated(endpoint, access)
	create := http.IdempotentAuthenticated(secured, store, definition)
	return create
}

// doc-example:end flat-routes-3
// doc-example:begin flat-routes-4
func pageRoutes(ctx context.Context, publicRoute http.Route[http.NoPath], guardedRoute http.Route[http.NoPath], adminGuard http.GuardBinding[Admin], service pageService) ([]http.RouteRegistration, string, error) {
	route := publicRoute
	query, body, response := http.EmptyQuery(), SavePageBody(), PageResponse(200)
	endpoint := http.DefineEndpoint(guardedRoute, query, body, response)
	showPage, createPage := service.Show, service.Create
	pathValue, queryValue := http.NoPath{}, http.NoQuery{}

	public := http.DefineEndpoint(route, query, body, response).Handle(showPage)
	admin := http.Authenticated(endpoint, adminGuard).Handle(createPage)
	url, err := http.DefineEndpoint(route, query, body, response).URL(ctx, pathValue, queryValue)
	return []http.RouteRegistration{public, admin}, url, err
}

// doc-example:end flat-routes-4
// doc-example:begin flat-routes-5
func pageEndpoint(create http.Route[http.NoPath]) http.Endpoint[http.NoPath, http.NoQuery, SavePageRequest, PageResource] {
	response := PageResponse(200)
	endpoint := http.DefineEndpoint(create, http.EmptyQuery(), SavePageBody(), response)
	return endpoint
}

// doc-example:end flat-routes-5
