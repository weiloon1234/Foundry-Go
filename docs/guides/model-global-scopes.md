# Model global scopes

Global scopes attach typed, additive predicates to a model declaration. A scope can enforce a tenant or visibility rule without depending on every caller to remember the filter.

## Declare and attach scopes

<!-- orm-example: scopes-after -->
```go
func scope(t reads.MemberTable) orm.GlobalScope[reads.Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[reads.Member], error) {
		id, err := tenantID(ctx)
		return t.TenantID.Eq(id), err
	})
}
```

In this [compiled example](../../tests/fixtures/goway/migration/examples_test.go), `tenantID` reads typed context metadata and returns an error when it is missing. It never substitutes a permissive tenant. Attach the source through the options passed to `orm.Define`:

<!-- orm-example: scope-options -->
```go
func scopeOptions() []orm.Option {
	return []orm.Option{orm.WithGlobalScopes(func(t reads.MemberTable) []orm.GlobalScope[reads.Member] {
		return []orm.GlobalScope[reads.Member]{scope(t)}
	})}
}
```

The lazy source receives the completed table declaration. It runs once when compilation first needs scopes; it may refer to initialized relations. It must build declarations without compiling or executing queries. Names must be valid identifiers and unique within the model. Declarations are immutable snapshots, not a mutable registry.

A source failure, panic or `runtime.Goexit` leaves a sticky failure. Later operations keep failing safely rather than running unscoped. Context predicate callbacks resolve on every execution, so reusing a query does not retain a previous request's tenant. Callback errors or invalid predicates fail closed before SQL.

A scope source is contained until its actual exit; panic/Goexit is classified as `fault.Panicked`. Recursive scope requirements are bounded by the compiler.

## Where scopes apply

Scopes apply to reads, counts, aggregates, pages, cursors, chunks, relations, joins, subqueries, CTEs, updates, deletes, lookup writes and source writes. Each source keeps its own scopes; a parent's bypass does not remove a target or pivot scope. Scope predicates remain inside joined sources so outer joins retain unmatched outer rows.

Inserts are unfiltered. Lookup creation derives supported equality defaults from active scopes. Upsert update actions include active destination scopes in their conflict condition. An unconditional update that skips a conflict outside those scopes fails rather than silently succeeding. An explicit update condition retains its documented skipped-result behavior.

Raw SQL bypasses automatic scopes, including complete-model `Table.Raw`. Retrieval callbacks on raw models do not restore tenant filtering. Applications own authorization for bypass and raw paths.

## Query-local bypass

<!-- orm-example: bypass-after -->
```go
func bypass() orm.Query[reads.Member] {
	return orm.WithoutGlobalScope(reads.Members.Query(), reads.MemberTenant(reads.Members))
}
```

`orm.WithoutGlobalScope(q, scopes...)` removes named defaults for that query. `orm.WithoutGlobalScopes(q)` removes all. Unknown or foreign scopes fail validation. These functions return derived values and do not mutate the declaration.

For relation targets, use `WithoutRelationGlobalScope` or `WithoutRelationGlobalScopes`. Through relations separately accept `WithoutPivotGlobalScope` or `WithoutPivotGlobalScopes`. They retain the exact descriptor kind and affect only that descriptor's target or pivot.

Execution, including explain, resolves scopes with its context. For SQL inspection, pass `orm.WithScopeContext(q, ctx)` to `orm.Compile`. Without required context, compilation fails closed. Recursive scope dependencies are bounded and rejected rather than evaluated indefinitely.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
