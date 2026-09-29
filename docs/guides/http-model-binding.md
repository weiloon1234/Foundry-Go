# Typed route-model binding

Model binding resolves decoded path keys into concrete models before a domain
handler runs. The original endpoint still owns parsing, validation, middleware,
errors, URL generation and the explicit response DTO.

The [independent consumer](../../tests/fixtures/consumer/httpmodels/bindings.go)
uses generated path/model/response declarations:

```go
type ShowRequest = modelbinding.Input[
    httpkernel.UserPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User,
]

func UserByID(db database.Executor) modelbinding.Resolver[httpkernel.UserPath, models.User] {
    return modelbinding.ByKey(
        db,
        models.QueryUsers().Where(models.UserFields().Status.Eq(models.StatusActive)),
        func(p httpkernel.UserPath) model.ID[models.User] { return p.User },
    )
}

func Router(db database.Executor, service Service) (*foundryhttp.Router, error) {
    bound := modelbinding.Bind(Show, UserByID(db))
    return foundryhttp.NewRouter(bound.Handle(service.Show))
}
```

`Show` is an ordinary typed endpoint. Its response is the generated
`httpdto.UserResponseJSON()` contract. `service.Show` receives `ShowRequest`:
`in.Request` retains the original path/query/body values, and `in.Model` is the
loaded `models.User`. There is no second lookup in the handler. Application
assembly supplies the database; no global request or dependency container is
required inside a model.

## Query behavior

`ByKey` calls the generated query's `Find` method once. The model and primary-key
types remain intact, including named numeric/string natural keys. The supplied
query retains filters, soft-delete visibility, eager relations and retrieval
hooks. Eager loads and hooks can perform their normal additional queries; one
binding resolution does not promise exactly one SQL statement in those cases.

A malformed path key returns 400 before database access. An omitted lookup result
returns 404. An existing model outside the declared filter or parent scope also
appears absent. Database failures and failed retrieval hooks remain errors and
never become an absent model. Framework endpoints resolve the model at their
binding stage (`HandleBound`): after request authorization and before
validation, as Laravel resolves route bindings before a FormRequest's rules. A
missing model is therefore 404, and a `WithAuthorization` resource denial 403,
even when the body would also fail validation (422). The handler receives the
model from that single lookup. A custom transport without `HandleBound` resolves
after validation instead. Idempotent model-bound endpoints resolve inside their
preparation, after validation. Signature checks on a `SignedEndpoint` still
happen before any path decoding or model resolution.

`WithMissing` replaces the default 404 for an omitted result, for example with a
declared application error or a 410 for archived resources:

```go
resolver := UserByID(db).WithMissing(func(ctx context.Context, p httpkernel.UserPath) error {
    return UserGone // a declared application error
})
```

The callback receives the decoded path, runs in an owned callback (panics become
internal errors) and is never invoked for lookup failures. Returning nil keeps 404.

There is no implicit transaction or row lock. Model writes and any required
transaction-scoped reads remain part of the domain operation. Locked query types
cannot be passed to `ByKey`, since their execution contract requires a transaction.
A later request resolves the model again; bindings retain no cross-request cache.

## Custom keys and parent scopes

Use `Define` for a slug, tenant-aware query, alternate key or child route. The
callback returns `value.Optional[Model]`, preserving the same missing/error rules.
The consumer's child resolver uses ordinary typed predicates:

```go
func OrderWithinUser(db database.Executor) modelbinding.Resolver[OrderPath, models.Order] {
    return modelbinding.Define(func(ctx context.Context, p OrderPath) (value.Optional[models.Order], error) {
        return models.QueryOrders().
            Where(models.OrderFields().BuyerID.Eq(p.User)).
            Find(ctx, db, p.Order)
    })
}
```

If domain logic needs a loaded parent before constructing the child's query,
call its typed resolver's `Resolve(ctx, path)` in the custom callback, then use
that concrete parent in the child scope. Return failures immediately. There is
no guessed relationship name, string field lookup or raw SQL involved. Permission
checks remain explicit domain/auth policies; matching a route key alone does not
authorize access.

## Models, getters and response DTOs

Loaded fields retain their stored values. Choose stored fields or typed getters
explicitly while constructing the declared response DTO, following the generated
field notices and [getter contracts](model-accessors.md) and [mutator contracts](model-mutators.md).
Model binding does not run getters implicitly or add model fields to the public
wire schema. Endpoint inspection remains the original transport contract.

Configure the original endpoint's scope, validation, limits, middleware and signer
before `Bind`. Continue using that descriptor for typed URL generation. Both
ordinary and signed endpoints share the same binding adapter.

## Failure and cancellation

Resolver callbacks receive the request context and must support concurrent
requests. `Resolve` returns no partial model when a lookup fails or cancellation
is observed. Panics and `runtime.Goexit` are contained as internal failures.
Cancellation never abandons an active callback or releases its resources early;
cooperative database cancellation and the existing HTTP timeouts still apply.

Zero resolvers, nil executors/queries/selectors, a nil missing handler and
invalid query declarations reject registration. Registration validates the whole
declaration once — each nested ancestor exactly once — without resolving a model
or opening a transaction; requests then resolve without revalidating. A direct
`Resolve` call validates once per call and resolves nested parents without
revalidating each level. Custom resolver errors preserve private causes;
response classification follows the endpoint's existing error declarations.


## Typed authentication composition (milestone 10)

`BindAuthenticated(transport, resolver)` combines a required or optional typed
authentication transport with a distinct bound resource. Its handler receives
`(context.Context, Subject, Input[P,Q,B,Resource])`. Required transports retain
their concrete authenticated model; optional transports retain `value.Optional[M]`.
Both ordinary and signed auth endpoints satisfy `http.AuthenticatedTransport`.

Authentication, token scopes, permissions and signatures execute before decoding
or model resolution. The shared resolver still runs once per request, at the
binding stage before validation; resource policies remain explicit. See [authentication composition](authentication.md#signed-endpoints-and-bound-resources)
and the [consumer](../../tests/fixtures/consumer/authenticating/composed_routes.go).
