# Typed model queries

Generated model queries now compile and execute PostgreSQL reads through Foundry's [database runtime](database-runtime.md). Handwritten models remain the source of field names, column mappings and value types. The [independent PostgreSQL consumer](../../tests/fixtures/consumer/model_query_postgres_test.go) exercises the complete public flow.

## Query construction and execution

```go
fields := models.UserFields()
users, err := models.QueryUsers().
    Where(fields.Age.Gte(18), fields.Status.Eq(models.StatusActive)).
    OrderBy(fields.Age.Asc()).
    Limit(50).
    All(ctx, db)
```

`QueryUsers()` returns a generated `UserQuery` wrapping `query.Query[User]`. Its fluent `Where`, `OrderBy`, `Limit` and `Offset` methods preserve the model-specific wrapper. Queries can be derived and compiled concurrently without changing their source. Execution uses an explicit `database.Executor`, so the same query works with a pool, transaction or connection session. Keep operations on a particular transaction/session sequential.

`All` returns a slice of complete models, empty on a successful no-match result. It discards the entire collected result on failure. `Each(ctx, executor, func(User) error)` streams one complete model at a time without eager clauses; with eager loading it uses bounded batches. Rows close on early callback return, error or panic. A callback may already have performed work when a later row or batch fails; these side effects are not rolled back automatically. See [bounded model iteration](model-chunks.md) for `Chunk`, `ChunkByID`, `EachChunked` and `EachByID`.

The normal Go collection result is `[]User`, usable with `range` and standard slice utilities. A query builder remains separate from the model and its slice of results. `Where` and `OrderBy` only derive a query; execution happens at terminal methods such as `All`, `First` and `RequireFirst`. `First` is optional, while `RequireFirst` returns a concrete model or an error. Plain model field access performs no database operation.

| Operation | Go result | Laravel counterpart |
| --- | --- | --- |
| `Where(...).OrderBy(...)` | Immutable typed query builder; no I/O | Query builder |
| `First(ctx, db)` | `(value.Optional[User], error)` | `first()` with explicit absence/error handling |
| `RequireFirst(ctx, db)` | `(User, error)` | `firstOrFail()` |
| `All(ctx, db)` | `([]User, error)` | `get()` |

The idiomatic Go collection is a typed slice, so callers can iterate with `for _, user := range users`. No custom collection wrapper is required to retain model typing. `All` is the collection terminal; the query builder has no duplicate `Get` synonym.

Loaded slices support in-memory operations through Go's standard [slices package](https://pkg.go.dev/slices) and `cmp`:

```go
// Imports: "cmp" and "slices".
ordered := slices.Clone(users)
slices.SortFunc(ordered, func(a, b models.User) int {
    return cmp.Compare(a.Email, b.Email)
})
```

`SortFunc` sorts in place; cloning first preserves the original slice order. The clone is shallow, so nested slices, pointers and other referenced values remain shared. `SortStableFunc` preserves the relative order of equal items. These operations execute no SQL and affect only loaded results. Use query `OrderBy` before pagination to order the full matching dataset; sorting a page in memory only rearranges that page. Domain methods still live on the model, while collection helpers can be ordinary typed functions over slices.

Laravel distinguishes `Model::all()` from a constructed query's `get()`; both retrieve models. Its collection's `all()` instead returns the underlying PHP array. Foundry's `All()` executes the current query and already returns `[]User`, so there is no separate collection-unwrapping step. `Count(ctx, db)` asks the database to count the selected query window; `len(users)` counts an already loaded slice without database work. See [Eloquent retrieval](https://laravel.com/framework/docs/13.x/eloquent#retrieving-models) and [collection all](https://laravel.com/framework/docs/13.x/collections#method-all).

`All` retains its results in memory. Use `Limit` for bounded collections, `Each` for iteration or explicit chunk methods for controlled batch sizes. Execution checks the caller's context and passes it through the database runtime. Pool acquisition retains its separate deadline. [Direct relations](model-relations.md) add `With`, explicit `Load` and `LoadMissing`; eager `Each` closes parent rows before loading each batch's relations and invoking callbacks.

## First rows and typed identities

`First` returns `value.Optional[User]`. An omitted result means no row matched; a zero model is never used as a missing-record sentinel. `RequireFirst` returns `database.NotFound` when the result is absent, discoverable with `errors.Is`.

Generated `Find(ctx, executor, model.ID[User])` and `RequireFind` apply the concrete primary-key type. A natural-key model uses its declared key type, such as `models.CountryCode`. The methods remain available after fluent query derivation and preserve its filters and pagination. A matching primary key outside the selected window is therefore absent.

`First` caps the selected window at one row and uses primary-key ascending order if no explicit order is present. A zero limit remains empty. For a stable business ordering with ties, add an explicit primary-key order after the business fields. [Numbered and cursor pagination](model-pagination.md) automatically append the primary tie-breaker when absent, preserve filters, and bound their page sizes. Separate requests do not share a database snapshot.

## Predicates and SQL compilation

Model fields preserve both model owner and value type. Predicates and orderings from another model fail compilation. Numeric and decimal fields expose range operations; text fields add `Like` and `Contains`; nullable fields expose `IsNull`/`IsNotNull`. `Eq` accepts the concrete non-null type. Generated field constructors attach the same [typed codecs](database-codecs.md) used for hydration, so malformed enum casts, integer overflow and invalid temporal precision fail before SQL execution.

`query.And`, `query.Or` and `Predicate.Not` preserve grouping. Empty logical junctions are invalid. `In()` with no values matches nothing, and its negation matches everything. SQL NULL semantics remain SQL's three-valued logic: use explicit null predicates when null rows must match.

[Conditional row values](conditional-expressions.md) add typed CASE, COALESCE and NULLIF expressions. Their comparisons remain model-owned row predicates, and `Value()` promotes a computed value for selection into a declared projection or scalar result. Selected aggregate/window counterparts retain their separate evaluation boundary.

`Contains` matches a literal substring, escaping `%`, `_` and its `!` escape character inside a bound parameter. `Like` accepts a PostgreSQL LIKE pattern with PostgreSQL's normal pattern semantics. Neither operation interpolates input into SQL text.

```go
u := models.UserFields()
matches, err := models.QueryUsers().Where(u.Email.Like("%john%")).All(ctx, db)
// For user-supplied text, match the literal substring, including any % or _.
matches, err = models.QueryUsers().Where(u.Email.Contains(searchTerm)).All(ctx, db)
```

Use `Like("john%")` for a prefix pattern and `Like("%@example.com")` for a suffix pattern. `All` is the collection terminal corresponding to Laravel's `get`; `First` returns an optional single model. Generated fields replace string field names, and typed operators replace string operator names. The [architecture's typed model boundary](../../blueprint/00-master-architecture-and-parity.md#agreed-architecture-decisions) applies to advanced queries as well as ordinary CRUD.

[Typed calculations](scalar-calculations.md) add arithmetic and text expressions such as `query.Lower(fields.Email).Contains("example")`. Row calculations also supply computed `OrderBy` keys for normal model reads and numbered/simple pagination; computed cursor keys require declared projection fields.

`Compile()` produces a `query.Statement` without connecting. `SQL()` and `Arguments()` are explicit inspection/adapter boundaries; argument slices are copied, and ordinary statement formatting omits bindings. Treat an explicitly retrieved argument list as application data. Table and column identifiers are validated and quoted, and predicates/orderings must reference declared columns. PostgreSQL's default 63-byte identifier bound is enforced before generation/compilation instead of accepting names the server would truncate. [PostgreSQL identifier rules](https://www.postgresql.org/docs/18/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS)

Expression depth, expression count, ordering count and parameter count are bounded by the query package's documented constants. The compiler uses one shared AST for every generated model. No string field lookup is needed in normal consumer queries.

## Aggregates and complete hydration

`Count` and `Exists` query the selected window, including `Limit` and `Offset`, without hydrating models. Derive a count from the unpaginated base when a total matching count is needed. `Exists` asks for at most one row. Separate aggregate and model queries do not promise a common snapshot; use an explicit transaction/isolation level when that consistency matters.

[Typed relation aggregates](model-aggregates.md) compute per-parent counts and numeric summaries in generated loaded slots, without hydrating related models. [Declared projections](model-projections.md) execute scalar summaries and grouped reports into separate complete result types.

[Distinct reads](model-distinct.md) remove duplicate selected records or choose one ordered record per typed key combination. These read-only selections preserve complete result types and count after deduplication.

Generated SELECTs enumerate every persisted column in declaration order, respecting column tags and omitting `foundry:"-"` fields. A fresh model is scanned through concrete typed codecs and published only when every field succeeds. SQL NULL, malformed stored enums, exact decimals and temporal values follow their codec contracts. Ignored fields retain their Go zero values.

Partial reads use [declared projection records](model-projections.md), which have their own generated selection types and complete decoders. `query.Define`/`ForModel` and field constructors are explicit declaration boundaries for generated code and custom integrations; custom decoders are responsible for complete, fresh hydration. `query.For` can still describe a table, but it cannot execute model reads without metadata and a decoder.

## Current limits and verification

This guide covers the read portion of milestone 05. [Model writes](model-writes.md) provide create/update/delete and required/default mutation validation; [pagination](model-pagination.md) provides numbered, simple and cursor model pages. Relations, computed aggregates, declared projections and advanced query composition are described in [milestone 06](../../blueprint/06-relations-and-advanced-queries.md), with current verification status in the master roadmap. Lifecycle integration remains milestone 07 work. Raw parameterized SQL remains available through `database.Executor`.

Compiler tests cover quoting, placeholder ordering, logical grouping, literal substring escaping, cloned bindings, resource bounds and concurrent derivation. Consumer compile-failure assertions include wrong typed `Find` keys after fluent derivation. Real PostgreSQL tests verify complete user/natural-key/decimal model hydration, nullable/self-reference IDs, empty results, filtered lookup, selected-window aggregates, early streaming exit, cancellation and malformed-row rejection. Real gopls checks inspect generated and promoted query methods in the consumer workspace.
