# Filter models through relationships

`orm.Has` and `orm.Not(orm.Has(...))` use [declared relationships](model-relations.md) to filter parent models. They compile to correlated EXISTS/NOT EXISTS through the shared query compiler. Applications supply typed descriptors and field predicates; Foundry supplies SQL aliases, key comparisons and parameter binding. The [independent consumer fixture](../../tests/fixtures/consumer/relationshipfilters/filters_postgres_test.go) demonstrates the public API.

## Matching and missing relations

The `models` namespace below is the application model package, with its generated
tables and [typed relationship declarations](../../tests/fixtures/consumer/models/orm_relations.go).
Pass any derived relation through the function parameter shown in the recipe.

The application owns `models.User`, `models.Order`, `models.Group`, and `models.Membership` and any generated tables or identities used below.

`models` is your application model package. Its `UserLinks` groups the declared
`Introducer` (belongs-to through `Users.IntroducerID`), `Referrals` (has-many
through that same key), `Orders` (has-many through `Orders.BuyerID`), and `Groups`
(through `Memberships`) relations. `GroupLinks.Owner` joins the group's owner to
`Users.ID`; `MembershipLinks.Inviter` joins the pivot's inviter to `Users.ID`.
These are application relation declarations built with `orm.BelongsToOf`,
`orm.HasManyOf`, and `orm.ManyToManyOf` using the generated slots and key columns.

`models.Status` is the application's string-backed `//foundry:enum`, with `StatusActive = "active"` and `StatusDisabled = "disabled"`; generate its enum descriptor with the model package.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database`, `database/orm`.

Inputs: `ctx context.Context`, `db database.Executor`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_filters.go#model-relationship-filters-1 -->
```go
orders := models.UserLinks.Orders.
	Where(orm.Gte(models.Orders.TotalCents, 1000))

buyers, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(orders)))
if err != nil {
	return err
}
others, err := orm.All(ctx, db, models.Users.Query().Where(orm.Not(orm.Has(orders))))
if err != nil {
	return err
}
```

This selects users with, or without, an order satisfying the filter. It uses one parent SQL statement, without loading related models or collecting IDs first. Several matching children do not multiply the parent rows. An absent nullable relationship key does not match SQL equality, so its parent qualifies for `orm.Not(orm.Has(...))`.

The same predicate functions accept belongs-to, has-one, has-many and many-to-many descriptors. They preserve the model query: pagination and scoped model writes remain available after filtering. A relationship owned by another parent model fails Go compilation.

Existence asks whether any matching row exists. It does not hydrate a singular relationship or assert that only one matching row exists. Loading a `One` slot still enforces its [cardinality contract](model-relations.md#loading-and-reading-values).

## Compose nested and alternative conditions

`orm.Has(relation)` returns a predicate owned by the parent model. This integrates with the ordinary boolean query API:

Inputs: `ctx context.Context`, `db database.Executor`, `orders orm.HasMany[models.User, models.Order]`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_filters.go#model-relationship-filters-2 -->
```go
u := models.Users
users, err := orm.All(ctx, db, models.Users.Query().Where(orm.Or(
	orm.Has(orders),
	orm.Eq(u.Status, models.StatusActive),
)))
```

Nested related filters use the same descriptors:

Inputs: `ctx context.Context`, `db database.Executor`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_filters.go#model-relationship-filters-3 -->
```go
referralsWithOrders := models.UserLinks.Referrals.
	Where(orm.Has(models.UserLinks.Orders.
		Where(orm.Gte(models.Orders.TotalCents, 1000))))

introducers, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(referralsWithOrders)))
```

Each `Where` accepts the related model's predicates. Nested self-relations work without application alias tags or SQL strings. Foundry chooses deterministic aliases that avoid names in the complete supplied query tree, including [explicit correlated subqueries](model-correlations.md).

## Filter many-to-many targets and pivots

Inputs: `ctx context.Context`, `db database.Executor`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_filters.go#model-relationship-filters-4 -->
```go
groups := models.UserLinks.Groups.
	Where(orm.Like(models.Groups.Name, "team%")).
	WherePivot(orm.Gte(models.Memberships.Priority, 2))

members, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(groups)))
```

Target and pivot predicates retain their separate model types. Both must match the same joined link. A pivot with a NULL key or absent target does not count as a related model. Duplicate links can satisfy existence without duplicating parents. Named natural keys retain their existing relation compatibility and codec contracts.

Target and pivot filters may themselves contain relationship predicates, such as a group's owner having orders or a membership's inviter meeting a condition. These nested scopes use the same alias analysis and correlation compiler.

## Reuse a descriptor for loading

Inputs: `ctx context.Context`, `db database.Executor`, `orders orm.HasMany[models.User, models.Order]`.

<!-- compiled-source: tests/fixtures/consumer/relationshipfilters/guide_filters.go#model-relationship-filters-5 -->
```go
users, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(orders)).With(orders))
```

`orm.Has` filters parents; `With` loads the matching orders into those returned models. Existence alone leaves relation slots unloaded. A descriptor's ordering and eager-loading clauses do not contribute to its existence test. They remain intact for `With`; existence construction does not mutate the descriptor. Use nested `.Where(orm.Has(child))` to filter by a deeper relationship, rather than relying on `.With(child)`.

Existence filters apply to normal scoped `Update` and `Delete` calls through the same parent predicate path. Soft-delete and global scopes remain part of each model query; authentication and authorization are separate concerns.

## Validation and resource behavior

The compiler checks bound relationship metadata and declared keys, then validates all nested source scopes and codecs before execution. Nil/zero relationships, invalid enum values, undeclared fields, malformed bindings and unsupported global relation windows fail explicitly. Eager-loading slot callbacks are not used by the existence operation.

Alias analysis and SQL compilation are bounded. Cyclic or excessive private query trees fail rather than recurse indefinitely. Bindings share one statement's parameter sequence and limits. Deriving or compiling another query does not alter captured descriptors or their predicates.

Execution uses the caller's context and executor. Canceled operations do not reach the database. Collected results are discarded on query/decoding failure, and ordinary transaction/savepoint outcomes still apply to [model writes](model-writes.md).

The master roadmap owns verification status; milestone 06 records delivered advanced-query contracts. Filtered model queries also support [bounded batch iteration](model-chunks.md).
