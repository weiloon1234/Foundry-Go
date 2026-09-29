# Model global scopes

A global scope is a typed default predicate that a model declares once and every query of that model applies, like Laravel's global scopes. Use it for rules that must not depend on each call site remembering a filter: a tenant boundary, published-only content, an archived flag. The [independent consumer](../../tests/fixtures/consumer/tenantqueries/models.go) declares a tenant scope and a published scope; its [PostgreSQL acceptance](../../tests/fixtures/consumer/tenantqueries/tenant_postgres_test.go) exercises them. The [office consumer](../../tests/fixtures/consumer/officequeries/models.go) declares a relation-existence scope (`Desk` is visible only while its office exists), accepted by its [PostgreSQL test](../../tests/fixtures/consumer/officequeries/desk_scope_postgres_test.go).

## Declaring scopes

```go
var (
    TenantScope = query.NewContextScope[Document]("tenant",
        func(ctx context.Context) (query.Predicate[Document], error) {
            tenant, ok := TenantFrom(ctx)
            if !ok {
                return query.Predicate[Document]{}, fault.New(fault.Invalid, "document queries require a tenant")
            }
            return DocumentFields().TenantID.Eq(tenant), nil
        })
    PublishedScope = query.NewGlobalScope[Document]("published", DocumentFields().Published.Eq(true))
)

func (Document) DefineGlobalScopes() []query.GlobalScope[Document] {
    return []query.GlobalScope[Document]{TenantScope, PublishedScope}
}
```

`NewGlobalScope` takes a fixed typed predicate. `NewContextScope` derives its predicate from the executing request: the function reads typed context metadata explicitly (here `TenantFrom(ctx)`), and must return an error when that metadata is absent rather than a permissive fallback. Declare `DefineGlobalScopes` next to the model with a value receiver and exactly the signature `func (Document) DefineGlobalScopes() []query.GlobalScope[Document]`, then run `foundry generate`; the generator reports a pointer receiver or any other signature against the method. Scope names use the SQL identifier grammar and must be unique per model. Keep scopes in package variables (or functions, below) so queries can remove them by value; there is no mutable registry.

The generated query passes `DefineGlobalScopes` uncalled. It runs once per process, on the first compilation that needs the model's scopes, after the model's generated query, fields and relations exist. A scope may therefore use the model's own relations:

```go
// A function, not a package variable: DeskRelations reads generated
// declarations that package initialization would evaluate too early.
func OpenOffice() query.GlobalScope[Desk] {
    return query.NewGlobalScope("open_office", DeskRelations().Office.Exists())
}

func (Desk) DefineGlobalScopes() []query.GlobalScope[Desk] {
    return []query.GlobalScope[Desk]{OpenOffice()}
}
```

`DefineGlobalScopes` must only build predicates: it must not compile or execute queries. A panic or invalid scope fails every query of the model with a validation error. A scope that (directly or through relations) requires itself is rejected by the compiler's nesting bound.

## Where scopes apply

Scopes join the same effective-predicate owner as [soft deletion](model-soft-deletes.md), so they reach every path that honors soft deletion:

- reads, `First`, `Count`, `Exists`, numbered/simple/cursor pagination, chunking and `Each`;
- relation eager loading, `WhereHas`/`WhereDoesntHave`, relation aggregates, many-to-many target and pivot filters, and joins, projections, subqueries and CTEs built from the model query;
- per-model and set-based updates and deletes, source writes, lookup writes and pivot operations.

Inserts are not filtered. An upsert's `DO UPDATE` changes only a conflicting row inside the destination's active scopes: the scopes join its conflict `WHERE`, so a tenant cannot overwrite another tenant's row through a globally unique key. When an unconditional update returns fewer rows than it wrote, the conflicting row lay outside the scopes and the upsert fails with `fault.Conflict` instead of silently returning nothing; with your own `Where`/row condition an omitted result keeps meaning "condition not satisfied" (see [upserts](model-upserts.md)). A scope on a relation target applies to that target inside the parent's query; the parent's own scopes are unaffected.

## Removing scopes

```go
drafts, err := QueryDocuments().WithoutGlobalScope(PublishedScope).All(ctx, db)
everything, err := QueryDocuments().WithoutGlobalScopes().Count(ctx, db)
related := DocumentRelations().Tags.WithoutGlobalScopes() // target scopes, this relation only
```

`WithoutGlobalScope(scopes...)` removes the named scopes from that query only, and `WithoutGlobalScopes()` removes all of them. Removing a scope the model does not declare fails validation. Relation descriptors offer the same pair for their target (`WithoutPivotGlobalScope`/`WithoutPivotGlobalScopes` for a many-to-many pivot), and locked queries keep them.

## Context scopes and compilation

Execution methods resolve context scopes with their own `ctx` on every call, including scopes lowered into `WhereHas` subqueries and relation loads. A query value reused across requests therefore never keeps an earlier request's tenant. Plan inspection (`Explain`) and cursor pagination resolve the same way. `Compile()` has no context: call `WithScopeContext(ctx)` first to inspect SQL, otherwise compilation fails closed with an error naming the scope. A scope function that panics or returns an error fails the operation; its result is validated against the model's table before use.

A lookup write's creation draft also receives defaults from active scopes (see [lookup writes](model-lookup-writes.md)), so `FirstOrCreate` in a tenant request creates the document for that tenant.
