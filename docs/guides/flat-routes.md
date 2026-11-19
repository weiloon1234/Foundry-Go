# Staged route declarations

Declare a route one step per statement, passing the previous value first. Reuse
the local when the result has the same type. Name a new local when authentication,
signing, replay or model binding changes the type.

```go
route := http.DefineRoute(spec, path)
route = http.ConfigureRoute(route, http.Within(adminScope))
endpoint := http.DefineEndpoint(route, query, body, response)
endpoint = http.ConfigureEndpoint(endpoint, http.Errors(PageProtected))
endpoint = http.PrepareEndpoint(endpoint, preparePage)
endpoint = http.ValidateBody(endpoint, pageRules)
admin := http.Authenticated(endpoint, adminGuard, http.Scopes(manage))
registration := admin.Handle(createPage)
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

For example, change:

```go
loginEndpoint := http.DefineEndpoint(routehelpers.Route(credentials, http.POST, "/login", "login", http.Public), http.EmptyQuery(), requests.AdminLoginBody(), native)
create := http.IdempotentAuthenticated(routeaccess.SecureCapture(http.DefineEndpoint(route, query, body, response), access), store, definition)
```

To:

```go
loginRoute := routehelpers.Route(credentials, http.POST, "/login", "login", http.Public)
loginEndpoint := http.DefineEndpoint(loginRoute, http.EmptyQuery(), requests.AdminLoginBody(), native)

endpoint := http.DefineEndpoint(route, query, body, response)
secured := routeaccess.SecureCapture(endpoint, access)
create := http.IdempotentAuthenticated(secured, store, definition)
```

Descriptors (`XBody()`, `XResponse(...)`, `http.EmptyQuery()`), options
(`http.Scopes(...)`, `http.Errors(...)`, `http.Within(...)`), scopes and
`RouteRegistration` values are not stages and may stay inline. Parentheses and
conversions cannot hide a nested stage call. Stage calls as statements,
assignments or return operands are allowed. Terminal methods on call results
are also allowed, including when the registration or URL is passed to another call:

```go
public := http.DefineEndpoint(route, query, body, response).Handle(showPage)
admin := http.Authenticated(endpoint, adminGuard).Handle(createPage)
url, err := http.DefineEndpoint(route, query, body, response).URL(ctx, pathValue, queryValue)
```

Use named stages before free-function terminals such as `pagination.Handle`.
Keep registrations in their original order. Stage package-level declarations
inside a typed initializer function when several statements are necessary.

## Generated HTTP pieces

Use the concrete helper beside a generated DTO declaration:

```go
response := resources.PageResponse(200)
endpoint := http.DefineEndpoint(create, http.EmptyQuery(), requests.SavePageBody(), response)
```

The `http` directive key selects transport independently of validation role:

```go
//foundry:dto http=body
type SavePageRequest struct { /* request fields */ }

//foundry:dto role=response http=response
type PageResource struct { /* response fields */ }
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
