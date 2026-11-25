package httpendpoints

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
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
type pageRequest = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, SavePageRequest]
type pageService interface {
	Create(context.Context, Admin, pageRequest) (PageResource, error)
	Show(context.Context, pageRequest) (PageResource, error)
}

// PageProtected is the safe public failure for edits to protected pages.
var PageProtected = foundryhttp.DefineError("page.protected", 409, "The page is protected.")

func preparePage(_ context.Context, in pageRequest) (foundryhttp.NoQuery, SavePageRequest, error) {
	in.Body.Title = strings.TrimSpace(in.Body.Title)
	return in.Query, in.Body, nil
}

// doc-example:end page-contracts

func registerPage(adminGuard foundryhttp.GuardBinding[Admin], manage auth.AccessScopes[Admin], service pageService) foundryhttp.RouteRegistration {
	// doc-example:begin page-stage-inputs
	spec := foundryhttp.RouteSpec{ID: "pages.create", Method: foundryhttp.POST, Access: foundryhttp.Guarded}
	path := foundryhttp.StaticPath("/pages")
	adminScope := foundryhttp.DefineScope("/admin", "admin")
	query, body, response := foundryhttp.EmptyQuery(), SavePageBody(), PageResponse(201)
	pageRules := validation.Rules(SavePageRequestValidationFields().Title, validation.NonBlank[string]())
	createPage := service.Create
	// doc-example:end page-stage-inputs

	// doc-example:begin flat-routes-1
	route := foundryhttp.DefineRoute(spec, path)
	route = foundryhttp.ConfigureRoute(route, foundryhttp.Within(adminScope))
	endpoint := foundryhttp.DefineEndpoint(route, query, body, response)
	endpoint = foundryhttp.ConfigureEndpoint(endpoint, foundryhttp.Errors(PageProtected))
	endpoint = foundryhttp.PrepareEndpoint(endpoint, preparePage)
	endpoint = foundryhttp.ValidateBody(endpoint, pageRules)
	admin := foundryhttp.Authenticated(endpoint, adminGuard, foundryhttp.AllOf(foundryhttp.Scopes(manage)))
	return admin.Handle(createPage)
	// doc-example:end flat-routes-1
}

func replayablePage(route foundryhttp.Route[foundryhttp.NoPath], access foundryhttp.GuardBinding[Admin], manage auth.AccessScopes[Admin], store *idempotency.Store, definition idempotency.Definition) foundryhttp.AuthenticatedIdempotentEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, SavePageRequest, Admin, PageResource] {
	query, body, response := foundryhttp.EmptyQuery(), SavePageBody(), PageResponse(201)

	// doc-example:begin flat-routes-3
	endpoint := foundryhttp.DefineEndpoint(route, query, body, response)
	secured := foundryhttp.Authenticated(endpoint, access, foundryhttp.AllOf(foundryhttp.Scopes(manage)))
	return foundryhttp.IdempotentAuthenticated(secured, store, definition)
	// doc-example:end flat-routes-3
}

func pageRoutes(ctx context.Context, publicRoute foundryhttp.Route[foundryhttp.NoPath], guardedRoute foundryhttp.Route[foundryhttp.NoPath], adminGuard foundryhttp.GuardBinding[Admin], manage auth.AccessScopes[Admin], service pageService) ([]foundryhttp.RouteRegistration, string, error) {
	route := publicRoute
	query, body, response := foundryhttp.EmptyQuery(), SavePageBody(), PageResponse(200)
	endpoint := foundryhttp.DefineEndpoint(guardedRoute, query, body, response)
	showPage, createPage := service.Show, service.Create
	pathValue, queryValue := foundryhttp.NoPath{}, foundryhttp.NoQuery{}

	// doc-example:begin flat-routes-4
	public := foundryhttp.DefineEndpoint(route, query, body, response).Handle(showPage)
	admin := foundryhttp.Authenticated(endpoint, adminGuard, foundryhttp.AllOf(foundryhttp.Scopes(manage))).Handle(createPage)
	url, err := foundryhttp.DefineEndpoint(route, query, body, response).URL(ctx, pathValue, queryValue)
	if err != nil {
		return nil, "", err
	}
	// doc-example:end flat-routes-4
	return []foundryhttp.RouteRegistration{public, admin}, url, nil
}

func pageEndpoint(create foundryhttp.Route[foundryhttp.NoPath]) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, SavePageRequest, PageResource] {
	// doc-example:begin flat-routes-5
	response := PageResponse(200)
	return foundryhttp.DefineEndpoint(create, foundryhttp.EmptyQuery(), SavePageBody(), response)
	// doc-example:end flat-routes-5
}
