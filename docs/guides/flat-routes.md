# Staged route declarations

Declare a route one step per statement, passing the previous value first. Reuse
the local when the result has the same type. Name a new local when authentication,
signing, replay or model binding changes the type.

The page DTOs are declared in [Generated HTTP pieces](#generated-http-pieces).
The handlers use this actor and request contract:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#page-contracts -->
```go
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
```

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-1 -->
```go
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
```

The existing typed functions and handler contracts remain the API. The request
runtime is unchanged.

## The rule

A call whose static result type is a route stage value must not be an argument
of any call. Pointers to stages and multi-result calls with any stage result are
covered too. This applies to application helpers, generic helpers, function values
and methods as well as framework functions. Assign the call result first.

Stage types come from the [declaration catalogue](../../internal/routestyle/catalog.go)
signatures: routes, endpoints, authenticated, idempotent, signed, model-bound,
paginated, datatable and raw stages, including binding transports and resolvers.
The checker derives their named type origins, so generic instantiation does not
change the rule. There is no second list of stage types. A `datatable.Table` is a
reusable declaration, not a route stage, so `datatable.Define(spec)` may stay inline.

For example, bind authentication before declaring replay:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-3 -->
```go
func replayablePage(route http.Route[http.NoPath], access http.GuardBinding[Admin], store *idempotency.Store, definition idempotency.Definition) http.AuthenticatedIdempotentEndpoint[http.NoPath, http.NoQuery, SavePageRequest, Admin, PageResource] {
	query, body, response := http.EmptyQuery(), SavePageBody(), PageResponse(201)

	endpoint := http.DefineEndpoint(route, query, body, response)
	secured := http.Authenticated(endpoint, access)
	create := http.IdempotentAuthenticated(secured, store, definition)
	return create
}
```

Descriptors (`XBody()`, `XResponse(...)`, `http.EmptyQuery()`), options
(`http.Scopes(...)`, `http.Errors(...)`, `http.Within(...)`), scopes and
`RouteRegistration` values are not stages and may stay inline. Parentheses and
conversions cannot hide a nested stage call. Stage calls as statements,
assignments or return operands are allowed. Terminal methods on call results
are also allowed, including when the registration or URL is passed to another call:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-4 -->
```go
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
```

Use named stages before free-function terminals such as `pagination.Handle`.
Keep registrations in their original order. Stage package-level declarations
inside a typed initializer function when several statements are necessary.

## Generated HTTP pieces

Use the concrete helper beside a generated DTO declaration:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-5 -->
```go
func pageEndpoint(create http.Route[http.NoPath]) http.Endpoint[http.NoPath, http.NoQuery, SavePageRequest, PageResource] {
	response := PageResponse(200)
	endpoint := http.DefineEndpoint(create, http.EmptyQuery(), SavePageBody(), response)
	return endpoint
}
```

The `http` directive key selects transport independently of validation role:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-6 -->
```go
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
```

`http=body`, `http=response`, and `http=both` select the generated
pieces. Without an explicit `http=` key, no helper is emitted. This explicit-only
default avoids compiling unused adapters. `role=response` retains its existing
validation meaning.

The helper stem removes one terminal `Request` or `Resource` suffix, then appends
`Body` or `Response`. Thus `SavePageRequest` produces `SavePageBody`,
`PageResource` produces `PageResponse`, and `UserResponse` produces
`UserResponseResponse`. A collision with a handwritten symbol or another planned
helper aborts generation and identifies both declarations. This includes all
same-package test declarations and import names, regardless of build constraints. External `_test` package collisions are left to the Go
compiler. There is no alternate
spelling chosen automatically.

JSON body helpers take no arguments. JSON response helpers take `status int`.
Multipart body helpers take `directory string`; use an empty string for the
existing default directory behavior. Their directive accepts `http=body`. Each
helper returns the same concrete `http.Body[T]` or
`http.Response[T]` as the existing adapter and wraps the existing descriptor.
Helpers have a non-inlining boundary so the DTO package owns the instantiation.

Framework packages keep the direct `http.JSONBody`, `http.JSONResponse` and
multipart adapters. Generated helpers and their enforcement apply to application
modules, including fixture modules. Generation rejects `http=` in the framework
module; endpoint and HTTP request/response scaffolds reject that destination
before creating files. Module ownership, not an import-path prefix, controls this
rule. Generic DTOs, unions, pagination adapters and datatable adapters do not
gain helpers.

## Application gate

From the application module:

```sh
go tool foundry routes check --dir .
```

The command loads Go packages, including tests, using their build configuration.
Optional package patterns follow the flags; the default is `./...`.
`--timeout 5m` bounds how long the CLI waits for loading and analysis. It returns
exit status 124 at the deadline. Go parsing and type checking cannot be cancelled;
those operations may continue in the analysis goroutine until the CLI process
exits. Run each module separately and include the build tags used by the
application's gate through `GOFLAGS`.

The check resolves static result types by package identity. Import aliases, dot
imports, explicit generic arguments and dynamically selected function values do
not change the rule. A same-named function returning a non-stage value is allowed.
Callback bodies are checked in their own execution context.

Findings name the file, line and edit to make. The command exits non-zero on
findings or package-loading errors. Generated route declarations are checked;
fix their generator rather than editing generated output. Handwritten
`JSONBody`, `JSONResponse` and multipart wrappers around generated descriptors
are findings only for concrete `foundry:dto` or `foundry:multipart` declarations
in the application module being checked, even when the key is absent. Checking
only a routes package still resolves metadata from its DTO dependencies. Message
declarations, declarations owned by another module and framework packages are
exempt. The diagnostic names the helper and says to add `http=body`, `http=response`, or `http=both`, regenerate,
and use it. Framework packages retain their direct adapters. Streams, raw
bodies, token/cookie responses and composed pagination/datatable adapters are not generated DTO declarations and are exempt.
