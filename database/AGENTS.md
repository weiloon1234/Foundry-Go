# Database and typed query changes

These instructions add to the [repository rules](../AGENTS.md). PostgreSQL is the
current database target. Consult the relevant query/model guide for the operation;
[database runtime](../docs/guides/database-runtime.md) and
[model writes](../docs/guides/model-writes.md) own lifecycle/outcome contracts.

## Typed persistence

- Preserve [typed declaration roles](../docs/guides/model-declarations.md):
  `CreatedAtOf`/`UpdatedAtOf` for managed timestamps, `Input*` for every mutated
  field, and primary roles through key adapters. `PrimaryOf` is needed only for
  primary insert-select targets. Require the exact declaration's `SoftDeletable`
  token for force-delete paths; it never grants visibility or authorization.
- Extend the existing AST, declaration metadata, PostgreSQL compiler and executor.
  Do not add a parallel SQL builder or interpolation path. Bind user values.
- Preserve model/key/field/projection/source ownership and codec value types through
  predicates, joins, relations and mutation results. Keep raw SQL an explicit escape
  hatch; missing ordinary typed operations belong in the shared query layer.
- Retain authorization/tenant filters, mandatory key predicates and soft-delete
  scopes. Do not silently discard unsupported read clauses during writes.
- Hydrate complete typed results through the existing codecs, or return failure.
  Do not expose partial models as successful reads/writes. Preserve narrow projections.
- `orm.WriteUnset` removes a pending assignment and restores omission, not NULL or
  zero. Preserve the typed input/storage distinction in [write drafts](../docs/guides/model-hooks.md).
  Reuse hook, mutator, validation, timestamp and observer owners; do not introduce
  a second lifecycle.
  Set-based operations must retain their explicitly documented lifecycle limits.
- `LoadFrom`/`LoadMissingFrom` use query-owned relation plans and limits on copies
  of supplied parents. They neither refetch nor filter those parents; callers own
  their provenance and authorization. Preserve loaded-empty slots and all-or-error
  attachment. See [existing-parent loading](orm/README.md#direct-eager-trees-and-existence).
- Keep bounded per-key adjustments identical with and without hooks/observers:
  missing or hidden keys return `database.NotFound`; visible bound rejection returns
  `orm.ErrBoundExceeded`. Only an empty guarded result adds one key-only locking
  read in the same transaction, retaining visibility predicates and locks but
  removing the bound. Preserve success statement counts; see [batch writes](../docs/guides/model-batch-writes.md).
- Preserve the [diagnostic redaction contract](orm/README.md#diagnostic-redaction)
  for `%v`, `%+v`, `%#v`, private/nested wrappers and error interpolation. Retained
  literals, bound data, assignment inputs, guard bounds, tokens, plan contents and
  pivot bindings stay redacted. Structural metadata (`LIMIT`/`OFFSET`, identifiers and SQL shape) may
  remain visible even when parameterized. Explicit inspection accessors disclose
  data; hydrated models and page items are not automatically redacted.

## Transactions and resource ownership

- Keep the same transaction/session and named connection throughout an operation.
  Nested scopes use existing savepoint behavior; inner success does not prove the
  outer commit. Read replicas do not promise immediate primary-write visibility.
- Preserve unknown/committed/not-committed outcomes and typed reconciliation
  candidates. A returned row or callback success alone does not prove commit.
  Never automatically replay a transaction callback to conceal uncertainty.
- Rows, pool slots, schema scope restoration and transaction cleanup must survive
  decoding, observer, cancellation and custom error-inspection failures. Apply the
  root callback/errorgraph rules at the owner that can finish rollback/release.
- Keep outbox/idempotent effects in the actual business transaction when required;
  do not substitute an after-commit closure for durable dispatch.

## Evidence to select for the completed batch

Use real PostgreSQL coverage for changed SQL, constraints, locking, commit outcomes
and scope restoration. Assert persisted state as well as errors. For type changes,
exercise independent consumer compilation and wrong-owner/type rejection. Query
performance work should check statement counts or representative plans/costs.
Use existing retained isolated test namespaces; the root data rules apply.
