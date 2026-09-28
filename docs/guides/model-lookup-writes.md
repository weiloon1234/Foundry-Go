# Lifecycle-aware lookup writes

`FirstOrCreate` and `UpdateOrCreate` operate on a typed model query within one transaction or savepoint. They return a complete concrete model and reuse normal hooks, observers, field mutators, managed timestamps and change tracking. These methods have different concurrency behavior from a [SQL upsert](model-upserts.md).

## Find an existing model or create one

```go
q := linkqueries.QueryLinkMemberships()
f := linkqueries.MembershipFields()
selected := q.Where(f.MemberID.Eq(member.ID), f.GroupCode.Eq(group.Code))
create := linkqueries.MembershipDraft{}.
    SetMemberID(member.ID).
    SetGroupCode(group.Code).
    SetRole(" ADMIN ")

membership, err := selected.FirstOrCreate(ctx, db, create)
```

The existing branch returns the first matching model in primary-key ascending order. It does not prepare the creation draft, allocate its UUID, invoke its setters, or run write hooks. Multiple matches intentionally use the same first-row ordering as an ordinary `First`; the helper does not prove that the lookup is unique.

The missing branch prepares the creation draft and runs ordinary creation. Query predicates retain stored value types, while creation setters retain their declared mutation input types. Foundry does not reverse-engineer arbitrary predicates into draft fields. Supply the required creation inputs explicitly; normal before hooks may fill omissions.

After creation, Foundry checks that the stored identity satisfies the original predicates and visibility. If normalization, a hook, a database default or a supplied value produces a model outside that scope, the operation fails and rolls back its creation and queued after-commit work. This postcondition does not automatically create a physical unique constraint.

## Update an existing model or create one

```go
membership, err := selected.UpdateOrCreate(ctx, db, create,
    func(ctx context.Context, tx *database.Tx, current linkqueries.Membership) (linkqueries.MembershipDraft, error) {
        return linkqueries.MembershipDraft{}.
            SetRole("editor-" + current.Role), nil
    })
```

Only the chosen branch prepares a draft. A missing model uses `create` and never invokes the update callback. An existing model invokes the callback and leaves the creation draft unused. Separate drafts keep creation-only primary keys out of updates.

The existing row is locked before the callback runs, and its result stream is closed first. The callback receives the actual transaction and can perform related typed database work on that connection. Propagate its supplied context to nested operations; it shares the normal lifecycle recursion bound with [per-model batch updates](model-batch-writes.md).

The ordinary update retains the original predicates plus the selected model's concrete primary identity. Its new field values may move the result outside the old filter, just as with a normal `Update`. A callback that changes the selected row itself can make the subsequent ordinary update fail; that failure rolls back the entire helper operation.

## Visibility and query options

Both helpers accept ordinary typed predicates and the model's active/trashed visibility. They reject read ordering, limits, offsets and eager-loading options before opening a transaction. Primary order selects one model; load relations explicitly on the returned model when needed.

Write-owned lookups skip `Retrieved` callbacks. Returned fields remain stored values, and [explicit getters](model-accessors.md) remain deliberate choices for DTO mapping. Foundry does not create a partially filled model or evaluate read getters implicitly.

Soft-deleted rows are hidden by default. A hidden row may still own a unique key, so a missing active lookup can fail on creation with `database.UniqueViolation`. `WithTrashed` and `OnlyTrashed` select their declared visibility, but do not silently restore anything. Creation must satisfy the selected visibility as well as its explicit predicates. Natural keys retain their declared types.

## Transactions and competing requests

The helper holds selected row locks until its owning transaction ends. Normal hooks execute inside that transaction. Callback, hook, codec, constraint, SQL, cancellation or creation-postcondition failures roll back the helper's changes and pending after-commit callbacks. An outer transaction remains the final commit owner, and in-process callbacks are not crash-durable dispatch.

Selecting no row does not lock the absence of a matching row. Two concurrent requests can both take the creation branch. Physical unique constraints govern uniqueness; the losing insert reports its ordinary constraint error. Foundry performs no hidden retry and does not swallow a uniqueness failure from another constraint. Without an appropriate unique constraint, concurrent creations can both succeed. Serializable isolation may instead report an ordinary serialization failure.

Inspect the ordinary typed `query.WriteError[Model]` for a committed callback failure or uncertain commit outcome before deciding whether to retry. The returned candidate is reconciliation data, not proof of persistence. Side effects outside the transaction retain the [normal hook limitations](model-hooks.md).

The [consumer examples](../../tests/fixtures/consumer/linkqueries/lookup_postgres_test.go) cover branch choice, lazy creation inputs, callback I/O, normal lifecycle, natural keys, scopes, visibility and rollback. The [concurrency fixtures](../../tests/fixtures/consumer/linkqueries/lookup_concurrency_postgres_test.go) check existing-row locks before callbacks and competing absent-row creations under a physical unique index. Compiler-rejection fixtures and actual gopls probes preserve concrete model/draft/callback contracts.
