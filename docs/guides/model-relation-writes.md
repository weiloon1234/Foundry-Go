# Typed relation writes

`ThroughRelation.Attach`, `Detach` and `ForceDetach` use the keys already declared by `query.ManyToMany`. Source models, target models and pivot drafts retain their concrete Go types. The [independent consumer](../../tests/fixtures/consumer/linkqueries/models.go) defines a member/group relationship with a typed `Membership` pivot; no application-side column maps or raw queries are needed.

## Attach a model

```go
groups := linkqueries.MemberRelations().Groups
membership, err := groups.Attach(ctx, db, member, group,
    linkqueries.MembershipDraft{}.SetRole(" ADMIN "))
```

`membership` is a complete `Membership` model. Foundry refreshes the stored member and group by their typed primary identities, then derives the pivot keys from the relationship metadata. Omitted draft keys receive those defaults before the usual UUID preparation and creating/saving hooks. The pivot's `MutateRole` setter, managed timestamps and local/provider observers run through the same pipeline as ordinary `Create`. The original draft and supplied endpoint structs remain unchanged.

Explicit draft inputs remain explicit. A hook or setter can transform them, but the stored result must still link the selected endpoints and satisfy the relationship's target/pivot filters. A failed postcondition rolls back the relation write and its queued after-commit callbacks. A distinct custom setter input cannot be manufactured from a stored key; supply that typed input in the draft when necessary. Generated `FoundryCreateMutation` and `query.CreateDraft[P]` are the framework integration boundary, not additional consumer configuration.

Repeated attachments create separate pivots unless a database constraint rejects the duplicate. Declare the domain's uniqueness policy in a migration. A uniqueness violation returns the ordinary classified database error and rolls back the attempted link.

## Detach and force detach

```go
removed, err := groups.Detach(ctx, db, member, group)
permanentlyRemoved, err := groups.WithTrashedPivot().
    ForceDetach(ctx, db, member, group)
```

Both results are `[]Membership`, one complete model for each selected pivot. `Detach` runs normal per-model deletion: soft-delete pivots remain stored, while ordinary pivots are physically removed. Soft deletion returns the stored post-write model; physical deletion returns its pre-write model. Duplicate links invoke separate hooks. All selected removals commit or roll back together, including when a later pivot hook fails. Detachment shares its bounded candidate selection and ordinary mutation runtime with [per-model batch writes](model-batch-writes.md).

`ForceDetach` requires a soft-delete pivot and runs its force-delete lifecycle. The selected pivot visibility remains in force: use `WithTrashedPivot` to include deleted links, or `OnlyTrashedPivot` to select only deleted links. Ordinary `Detach` changes active pivots even with `WithTrashedPivot`; `OnlyTrashedPivot().Detach` therefore selects nothing. To restore a known pivot, use its ordinary generated `Restore` method with the typed pivot ID. See [soft deletion](model-soft-deletes.md) for event order and visibility.

## Filters, keys and bounded writes

```go
adminGroups := linkqueries.MemberRelations().Groups.
    Where(linkqueries.GroupFields().Name.Eq("staff")).
    WherePivot(linkqueries.MembershipFields().Role.Eq("admin")).
    WithWriteLimit(50)
removed, err := adminGroups.Detach(ctx, db, member, group)
```

Target and pivot filters retain their model ownership. Endpoint arguments identify existing rows; stale non-primary relation keys are refreshed before deriving the link. A missing or filtered-out endpoint returns `database.NotFound`. The source must be active. Target `WithTrashed`/`OnlyTrashed` visibility is explicit and independent of pivot visibility. NULL endpoint relation keys fail with `fault.Missing`. A non-primary target key matching multiple targets within the relation scope returns `database.TooManyRows`; migrations must enforce uniqueness when concurrent inserts could create ambiguity.

Detach locks and reads the matching pivot set in primary-key order before calling a pivot hook. `query.MaxRelationWriteRows` shares the existing atomic insert row budget. `WithWriteLimit` can lower that bound; an oversized set fails before any pivot mutation rather than returning a truncated success. It does not alter read limits. Ordering and eager-loading options are rejected for writes; use a descriptor containing the required filters and visibility.

## Transactions and concurrency

Relation writes accept the same `database.Transactor` capability as ordinary model writes, including a pool, a transaction or a wrapper. Foundry owns the transaction/savepoint for the operation. A veto, SQL/codec error, cancellation or failed stored-link check rolls back its database changes and pending after-commit work. An enclosing transaction still owns the final commit. If commit has an unknown outcome or an after-commit callback fails after a confirmed commit, the ordinary typed `query.WriteError[P]` (or `query.WriteError[[]P]` for detachment) retains the candidate result; inspect the outcome before deciding whether to retry. See [model write outcomes](model-writes.md). Callback side effects outside that database transaction retain the ordinary [hook limitations](model-hooks.md).

Source and target rows are refreshed under shared row locks, held through the relation transaction. When the caller supplies a transaction, those locks remain until its outer commit or rollback. Internal ownership reads skip `Retrieved` callbacks. Detach locks its complete bounded candidate set before mutation; it does not claim to remove links inserted after that selection. Hook code may perform more database work in the same transaction, and ordinary database deadlock/conflict errors remain possible. A pre-write cardinality check does not replace physical uniqueness constraints.

Attach validates the actual stored pivot through the existing relationship-existence AST after its normal write pipeline. This covers explicit key inputs, transformed values and scoped links without introducing a separate SQL compiler. Detach invokes the same single-model mutation pipeline for each locked candidate; hooks, changes and after-commit behavior are not duplicated in a relation-specific implementation.

## Acceptance coverage

The [lifecycle consumer](../../tests/fixtures/consumer/linkqueries/lifecycle_postgres_test.go) covers derived inputs, mutators, timestamps, hook order, effective changes, duplicate links, row limits, soft/force deletion, veto rollback, cancellation and outer-commit callbacks. The [key consumer](../../tests/fixtures/consumer/linkqueries/keys_postgres_test.go) exercises stale natural keys, ambiguous/nullable keys, self-links, visibility and transactor wrappers. The [concurrency consumer](../../tests/fixtures/consumer/linkqueries/concurrency_postgres_test.go) checks endpoint locks through hooks and competing attachments under a unique constraint. Compiler failures reject wrong source/target models, pivot drafts, result models and default ownership; real gopls probes inspect the concrete public signatures.
