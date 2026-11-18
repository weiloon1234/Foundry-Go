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

A route-declaring call must not occur inside an argument of another
route-declaring call. This includes route and endpoint construction and
configuration, authentication, optional authentication, signing, tickets,
idempotency, pagination, model binding and datatable route declarations.
The [function catalogue](../../internal/routestyle/catalog.go) is shared by the
check and the migration tool.

Option constructors such as `http.Errors`, `http.Scopes` and `http.Within`, DTO
descriptors, and body/response helpers are not route-declaring calls. Terminal
methods on call results are allowed:

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
validation meaning. The migration
tool derives transport direction from actual typed uses without changing role.

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
gain helpers in this release.

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

The check resolves function objects by import path. Import aliases, dot imports,
explicit generic arguments and statically resolvable function aliases do not
change the rule. Unrelated functions with matching names are allowed. It does
not attempt interprocedural analysis of dynamically selected function values.
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

## Migration

Replace nested declarations such as this pre-v0.45.0 form:

```go
admin := http.Authenticated(http.DefineEndpoint(create, http.EmptyQuery(),
    http.JSONBody(requests.SavePageRequestJSON()),
    http.JSONResponse(200, resources.PageResourceJSON())), guard)
```

After adding the transport keys and regenerating, declare each stage:

```go
response := resources.PageResponse(200)
endpoint := http.DefineEndpoint(create, http.EmptyQuery(), requests.SavePageBody(), response)
admin := http.Authenticated(endpoint, guard)
```

After updating the framework, run the external codemod with `--annotate --write`
to infer transport keys. Regenerate declarations, then run the codemod with
`--write` to replace wrappers and stage routes. Review both reports and run
`gofmt`. Resolve unsupported constructs before adding the check to the
application gate. The codemod is a temporary
migration tool and is not shipped with the framework CLI.

The codemod assumes route declarations, option constructors and other factories
used in declaration arguments are order-independent. It may hoist them across
earlier calls and reports each assumption as `reordered-call`. Review that report
for factories with side effects: registration, appends, map writes, logging,
resource acquisition, `sync.Once`, clock or random reads. It refuses visible
conflicting writes, channel operations, immediate closures, `defer`/`go`,
unsupported scope changes and types it cannot preserve, leaving the affected file
unchanged. This is not a whole-program effect proof. Prove route descriptors and
the exported client contract byte-identical before applying each application
migration.

Numbered fallback names still require readability review. A second successful
pass should report no changes.
