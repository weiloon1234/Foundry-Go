# Staged route declarations

Declare a route one step per statement, passing the previous value first. Reuse
the local when the result has the same type. Name a new local when authentication,
signing, replay or model binding changes the type.

`Admin` is the application's actor model. The setup below supplies the concrete
request, response, guard, scope and handler used by the eight stage statements.

<details>
<summary>Declarations and inputs for the stage sequence</summary>

Framework imports from `github.com/weiloon1234/Foundry-Go`: `auth`, `http` as `foundryhttp`, `idempotency`, `validation`.

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

The page handlers receive the authenticated `Admin` and use these request and service types:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#page-contracts -->
```go
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
```

Run `foundry generate` for the DTOs. `SavePageBody()` and `PageResponse(status)`
are their generated transport helpers. Supply `adminGuard http.GuardBinding[Admin]`,
`manage auth.AccessScopes[Admin]`, and `service pageService` to assembly.

Inputs: `service pageService`.

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#page-stage-inputs -->
```go
spec := foundryhttp.RouteSpec{ID: "pages.create", Method: foundryhttp.POST, Access: foundryhttp.Guarded}
path := foundryhttp.StaticPath("/pages")
adminScope := foundryhttp.DefineScope("/admin", "admin")
query, body, response := foundryhttp.EmptyQuery(), SavePageBody(), PageResponse(201)
pageRules := validation.Rules(SavePageRequestValidationFields().Title, validation.NonBlank[string]())
createPage := service.Create
```

</details>

Inputs: `adminGuard foundryhttp.GuardBinding[Admin]`, `manage auth.AccessScopes[Admin]`.

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-1 -->
```go
route := foundryhttp.DefineRoute(spec, path)
route = foundryhttp.ConfigureRoute(route, foundryhttp.Within(adminScope))
endpoint := foundryhttp.DefineEndpoint(route, query, body, response)
endpoint = foundryhttp.ConfigureEndpoint(endpoint, foundryhttp.Errors(PageProtected))
endpoint = foundryhttp.PrepareEndpoint(endpoint, preparePage)
endpoint = foundryhttp.ValidateBody(endpoint, pageRules)
admin := foundryhttp.Authenticated(endpoint, adminGuard, foundryhttp.AllOf(foundryhttp.Scopes(manage)))
return admin.Handle(createPage)
```

The existing typed functions and handler contracts remain the API. The request
runtime is unchanged.

<a id="staged-routes"></a>
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

Do not pass a call to `http.Authenticated` directly as the first argument of
`http.IdempotentAuthenticated`, or nest `http.DefineEndpoint` inside either call.
Assign each result first, as below. `query`, `body`, and `response` are
`http.EmptyQuery()`, `SavePageBody()`, and `PageResponse(201)`.

Inputs: `route foundryhttp.Route[foundryhttp.NoPath]`, `access foundryhttp.GuardBinding[Admin]`, `store *idempotency.Store`, `definition idempotency.Definition`.

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-3 -->
```go
endpoint := foundryhttp.DefineEndpoint(route, query, body, response)
secured := foundryhttp.Authenticated(endpoint, access, access.AnyActor())
return foundryhttp.IdempotentAuthenticated(secured, store, definition)
```

`route` is the public `http.Route[http.NoPath]`; `endpoint` is the guarded endpoint assembled above. Use `query = http.EmptyQuery()`, `body = SavePageBody()`, and `response = PageResponse(200)`. `showPage` and `createPage` are `service.Show` and `service.Create`; `pathValue` and `queryValue` are `http.NoPath{}` and `http.NoQuery{}`.

Inputs: `ctx context.Context`, `adminGuard foundryhttp.GuardBinding[Admin]`.

Descriptors (`XBody()`, `XResponse(...)`, `http.EmptyQuery()`), options
(`http.Scopes(...)`, `http.Errors(...)`, `http.Within(...)`), scopes and
`RouteRegistration` values are not stages and may stay inline. Parentheses and
conversions cannot hide a nested stage call. Stage calls as statements,
assignments or return operands are allowed. Terminal methods on call results
are also allowed, including when the registration or URL is passed to another call:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-4 -->
```go
public := foundryhttp.DefineEndpoint(route, query, body, response).Handle(showPage)
admin := foundryhttp.Authenticated(endpoint, adminGuard, adminGuard.AnyActor()).Handle(createPage)
url, err := foundryhttp.DefineEndpoint(route, query, body, response).URL(ctx, pathValue, queryValue)
if err != nil {
	return nil, "", err
}
```

Use named stages before free-function terminals such as `pagination.Handle`.
Keep registrations in their original order. Stage package-level declarations
inside a typed initializer function when several statements are necessary.

<a id="generated-helpers"></a>
## Generated HTTP pieces

Inputs: `create foundryhttp.Route[foundryhttp.NoPath]`.

Use the concrete helper beside a generated DTO declaration:

<!-- compiled-source: tests/fixtures/consumer/httpendpoints/flat_route_recipes.go#flat-routes-5 -->
```go
response := PageResponse(200)
return foundryhttp.DefineEndpoint(create, foundryhttp.EmptyQuery(), SavePageBody(), response)
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

A finding prints as `file:line:column: [rule] subject: problem; fix. Read
docs/guides/flat-routes.md#anchor`, where the guide and anchor teach the fix. In an
application, `(skill: name)` follows when a skill declares that anchor in a
`Foundry guides:` line. The rules are `route-nesting` (the staged-route rule above)
and `route-helper` or `route-helper-missing` (the generated helper rule below). The
command exits non-zero on findings or package-loading errors. Generated route declarations are checked;
fix their generator rather than editing generated output. Handwritten
`JSONBody`, `JSONResponse` and multipart wrappers around generated descriptors
are findings only for concrete `foundry:dto` or `foundry:multipart` declarations
in the application module being checked, even when the key is absent. Checking
only a routes package still resolves metadata from its DTO dependencies. Message
declarations, declarations owned by another module and framework packages are
exempt. The finding names the wrapped declaration and says to add `http=body`, `http=response`, or `http=both`, run `foundry generate`,
and call the helper. Framework packages retain their direct adapters. Streams, raw
bodies, token/cookie responses and composed pagination/datatable adapters are not generated DTO declarations and are exempt.
