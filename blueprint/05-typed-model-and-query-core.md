# 05 — Typed model and query core

## Purpose and prerequisites

Prerequisite: [04](04-database-runtime-and-migrations.md). Model fields, keys,
queries, assignments and results retain their concrete Go types. Stage 8 completes
the transition to handwritten declarations; historical milestone evidence stays in
the [master](00-master-architecture-and-parity.md#milestone-05-pagination-and-completion-evidence).

## Public type design

Declare tables with `orm.Define` and typed carriers bound to model fields. Primary
keys retain their owner and codec. Input, stored, managed and soft-delete roles
are distinct capabilities. `Query[M]`, predicates and orderings retain owner types;
`orm/expr` supplies expression phases and `orm/rows` supplies projections and sets.
See the [compiled migration examples](../docs/guides/orm-migration.md).

```go
users, err := models.Users.Query().Where(models.Users.Email.Eq(email)).All(ctx, db)
```

Wrong-owner predicates, wrong-value assignments and null assignment to a required
field fail compilation. Model hydration reads complete records; selected DTOs use
row projections. Omitted assignments, present zero and present null are distinct.
Query derivation is immutable and terminal operations take explicit context and
executor. Private SQL erasure never becomes an untyped consumer API.

## Execution and mutation contract

[Typed codecs](../docs/guides/database-codecs.md) own bind/scan validation. Enums
retain named types. Integer widths and signed SQL ranges are checked; decimals
never pass through floating point. NULL requires an explicit nullable carrier.
Dates, wall time and instants stay distinct. Failed hydration returns no partially
decoded models. SQL identifiers come from validated declarations and values bind
as parameters through the shared compiler.

[Model reads](../docs/guides/model-queries.md) preserve explicit missing results,
window-aware count/existence, deterministic order and retrieval callbacks.
`Limit(0)` is empty. Bounded streaming keeps batch boundaries, cancellation and
callback error identity. [Model writes](../docs/guides/model-writes.md) share the
transactional mutation owner, managed fields, hooks and observer validation.
Keyed writes reject ambiguous/malformed returning rows; typed outcome errors keep
reconciliation identity when commit confirmation is uncertain. A transaction
opens a transaction or a nested savepoint without changing these semantics.

[Pagination](../docs/guides/model-pagination.md) keeps separate numbered-page
count/row queries and one limit-plus-one cursor query. Cursor ordering is
null-aware and includes a unique tie-breaker; backward pages retain canonical
order. Tokens preserve model identity and are not authorization or snapshots.
Callers choose repeatable-read transactions when they need a shared snapshot.
The public cursor descriptor uses the native ORM owner; regenerate client schemas.

## Acceptance owners

- [ORM declarations and reads](../database/orm): role/ownership negatives, immutable
  derivation, codecs, lifecycle and atomic hydration.
- [Shared SQL core](../internal/querycore): canonical compilation, binding budgets,
  source/correlation validation, grouping and distinct planning.
- [Shared execution core](../internal/ormcore): bounded delivery and callback identity.
- [Independent consumers](../tests/fixtures/goway): compiler catalogue, public
  surface/footprint budgets, PostgreSQL parity and frozen historical observations.
- [Native model consumer](../tests/fixtures/consumer/models): handwritten declarations
  and hydration. DTOs and clients remain generated through the Makefile.

Consumer code owns domain models, schemas, authorization predicates and transaction
snapshots. Framework owners supply SQL, binding, lifecycle, hydration and pagination.
