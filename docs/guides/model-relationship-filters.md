# Filter models through relationships

`WhereHas` and `WhereDoesntHave` use [declared relationships](model-relations.md) to filter parent models. They compile to correlated EXISTS/NOT EXISTS through the shared query compiler. Applications supply typed descriptors and field predicates; Foundry supplies SQL aliases, key comparisons and parameter binding. The [independent consumer fixture](../../tests/fixtures/consumer/relationshipfilters/filters_postgres_test.go) demonstrates the public API.

## Matching and missing relations

```go
orders := models.UserRelations().Orders.
    Where(models.OrderFields().TotalCents.Gte(1000))

buyers, err := models.QueryUsers().WhereHas(orders).All(ctx, db)
others, err := models.QueryUsers().WhereDoesntHave(orders).All(ctx, db)
```

This selects users with, or without, an order satisfying the filter. It uses one parent SQL statement, without loading related models or collecting IDs first. Several matching children do not multiply the parent rows. An absent nullable relationship key does not match SQL equality, so its parent qualifies for `WhereDoesntHave`.

The same methods accept belongs-to, has-one, has-many and many-to-many descriptors. They preserve the generated model query wrapper: typed `Find`, `RequireFind`, pagination and normal model writes remain available after filtering. A relationship owned by another parent model fails Go compilation.

Existence asks whether any matching row exists. It does not hydrate a singular relationship or assert that only one matching row exists. Loading a `One` slot still enforces its [cardinality contract](model-relations.md#loading-and-reading-values).

## Compose nested and alternative conditions

Every relationship also exposes `Exists()`, returning a predicate owned by its parent model. This integrates with the ordinary boolean query API:

```go
u := models.UserFields()
users, err := models.QueryUsers().Where(query.Or(
    orders.Exists(),
    u.Status.Eq(models.StatusActive),
)).All(ctx, db)
```

Nested related filters use the same descriptors:

```go
referralsWithOrders := models.UserRelations().Referrals.
    Where(models.UserRelations().Orders.
        Where(models.OrderFields().TotalCents.Gte(1000)).Exists())

introducers, err := models.QueryUsers().WhereHas(referralsWithOrders).All(ctx, db)
```

Each `Where` accepts the related model's predicates. Nested self-relations work without application alias tags or SQL strings. Foundry chooses deterministic aliases that avoid names in the complete supplied query tree, including [explicit correlated subqueries](model-correlations.md).

## Filter many-to-many targets and pivots

```go
groups := models.UserRelations().Groups.
    Where(models.GroupFields().Name.Like("team%")).
    WherePivot(models.MembershipFields().Priority.Gte(2))

members, err := models.QueryUsers().WhereHas(groups).All(ctx, db)
```

Target and pivot predicates retain their separate model types. Both must match the same joined link. A pivot with a NULL key or absent target does not count as a related model. Duplicate links can satisfy existence without duplicating parents. Named natural keys retain their existing relation compatibility and codec contracts.

Target and pivot filters may themselves contain relationship predicates, such as a group's owner having orders or a membership's inviter meeting a condition. These nested scopes use the same alias analysis and correlation compiler.

## Reuse a descriptor for loading

```go
users, err := models.QueryUsers().WhereHas(orders).With(orders).All(ctx, db)
```

`WhereHas` filters parents; `With` loads the matching orders into those returned models. Existence alone leaves relation slots unloaded. A descriptor's ordering and eager-loading clauses do not contribute to its existence test. They remain intact for `With`; existence construction does not mutate the descriptor. Use nested `.Where(child.Exists())` to filter by a deeper relationship, rather than relying on `.With(child)`.

Existence filters apply to normal scoped `Update` and `Delete` calls through the same parent predicate path. Authentication and soft-deletion scopes must still be applied explicitly until their owning integrations arrive.

## Validation and resource behavior

The compiler checks generated relationship metadata and declared keys, then validates all nested source scopes and codecs before execution. Nil/zero relationships, invalid enum values, undeclared fields, malformed bindings and unsupported global relation windows fail explicitly. Eager-loading slot callbacks are not used by the existence operation.

Alias analysis and SQL compilation are bounded. Cyclic or excessive private query trees fail rather than recurse indefinitely. Bindings share one statement's parameter sequence and limits. Deriving or compiling another query does not alter captured descriptors or their predicates.

Execution uses the caller's context and executor. Canceled operations do not reach the database. Collected results are discarded on query/decoding failure, and ordinary transaction/savepoint outcomes still apply to [model writes](model-writes.md).

The master [roadmap](../../blueprint/00-master-architecture-and-parity.md) owns verification status; [milestone 06](../../blueprint/06-relations-and-advanced-queries.md) records delivered advanced-query APIs and remaining contracts. Filtered model queries also support [bounded batch iteration](model-chunks.md).
