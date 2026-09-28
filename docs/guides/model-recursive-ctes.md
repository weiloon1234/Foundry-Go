# Typed recursive CTEs

`RecursiveCTE` builds a recursive query from a complete model or declared projection. An anchor supplies initial records; a callback constructs the step using `RecursiveSelf[Record]`. Both inputs must return the same Go record type. The result is an ordinary `CommonTable[Record]`, reusable through aliases, joins, projections, sets and typed subqueries.

The [consumer fixture](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go) exercises model hierarchies, declared reports, natural keys, duplicate paths, cycles and cancellation against PostgreSQL.

## Read a model hierarchy

```go
type parentAlias struct{}
type childAlias struct{}
type resultAlias struct{}

anchor := models.QueryUsers().Where(models.UserFields().ID.Eq(rootID))
tree := query.RecursiveCTE("descendants", anchor,
    func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
        parent := query.As[parentAlias](self, "parent")
        child := query.As[childAlias](models.QueryUsers(), "child")
        p := models.UserFieldsAt(parent.Scope())
        c := models.UserFieldsAt(child.Scope())
        joined := query.InnerJoin(child, parent, query.On(c.IntroducerID, p.ID))
        return query.SelectRecord(joined, query.LeftScope(joined, child.Scope()))
    },
)

result := query.As[resultAlias](tree, "result")
fields := models.UserFieldsAt(result.Scope())
users, err := query.SelectRecord(result, result.Scope()).
    OrderBy(fields.Email.Asc()).All(ctx, db)
```

The anchor is included in the result. `users` is `[]models.User`, decoded with the model's original ordered codecs. Relations and computed slots remain unloaded. Use the normal explicit model loading operations when those are needed. Generated fields retain ID ownership, enum restrictions and nullable values in both the recursive step and the consuming query.

The callback runs once, synchronously, while building the declaration. PostgreSQL performs the subsequent iterations. It is a query-construction callback, not a per-row hook. Callback panics have ordinary Go behavior; the framework does not turn them into database errors.

`SelectRecord` selects a complete preserved model from a join. A custom recursive row uses a declared projection for both anchor and step; generated `ProjectResult` methods check each selected field. The step may read different underlying models while returning that same result record. No result-column list or untyped field map is required.

## Duplicate paths and termination

`RecursiveCTE` uses `UNION`, removing duplicate complete rows across iterations. `RecursiveAllCTE` uses `UNION ALL` and retains repeated paths. For an unchanged finite model graph, deduplication can terminate a cycle. Changing values such as an accumulated depth can keep rows distinct, so `UNION` is not a general cycle guard. These follow [PostgreSQL recursive evaluation](https://www.postgresql.org/docs/18/queries-with.html#QUERIES-WITH-RECURSIVE).

Apply traversal predicates inside the step when they should prune further expansion. A consuming filter affects returned rows. Order the consuming query explicitly; evaluation order is not an output-order contract. Input limits apply to their SELECT, not to recursion depth. An outer `Limit` may fail to stop traversal when sorting, joining or aggregation requires the full result. Execute potentially expensive recursion with a context deadline.

Foundry does not add hidden depth/path fields, an implicit maximum iteration count, or `SEARCH`/`CYCLE` clauses. AST depth/node/parameter limits bound query construction and compilation, not data traversal. Use domain termination rules; query cancellation remains the execution boundary. Cancellation may invalidate a transaction or connection, so follow the [database runtime](database-runtime.md) error and ownership contracts.

`Each` closes rows when the callback returns an error, but driver cleanup may drain remaining results. For immediate interruption of potentially endless server work, cancel a child query context in the callback before returning the error. Keep an overall deadline as well. The caller's context and transaction lifecycle remain explicit; ordinary callback failure does not silently cancel a shared transaction.

## Ownership and PostgreSQL constraints

Each step must contain exactly one reference to its own working table. The anchor cannot contain that reference. `RecursiveSelf` has no execution methods; capturing it in Go and later wrapping it in an executable query is rejected before executor access. A separately declared CTE cannot capture another declaration's working table. Ordinary dependencies remain reusable and are emitted once before their consumers.

Self-references in `IN`, `EXISTS` or scalar expression subqueries, nullable outer-join sides, `INTERSECT ALL`, either side of `EXCEPT ALL`, or the right side of `EXCEPT` are rejected. A derived `FROM` source may retain a valid self-reference. Aggregates are rejected at a SELECT level that directly references the working table. The compiler follows PostgreSQL's [recursion placement rules](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/parser/parse_cte.c) and [aggregate restrictions](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/parser/parse_agg.c).

`Materialized()` preserves the recursive definition. `NotMaterialized()` is rejected for recursive CTEs because it cannot provide inlining. Conflicting definitions, invalid names, missing/zero inputs and incompatible record layouts retain ordinary [CTE validation](model-ctes.md#validation-and-current-limits). Go types and generated declarations cannot prove the physical schema's SQL types, typmods or collations; PostgreSQL still validates their compatibility.

Recursive bodies are read-only. Their resulting IDs may constrain ordinary typed model writes, using the same transaction and query compiler. Additional SQL expressions, windows, locking and other advanced capabilities remain in [milestone 06](../../blueprint/06-relations-and-advanced-queries.md).
