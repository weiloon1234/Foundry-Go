# Bounded per-model batch writes

Use `CreateEach`, `UpdateEach`, `DeleteEach`, `RestoreEach` and `ForceDeleteEach` when every row needs its ordinary model lifecycle. They return ordinary typed slices and run normal hooks, provider observers, field mutators, timestamps and change capture. The [consumer examples](../../tests/fixtures/consumer/linkqueries/each_postgres_test.go) use the existing membership model and its concrete draft.

## Creating models

```go
draft := linkqueries.MembershipDraft{}.
    SetMemberID(member.ID).
    SetGroupCode(group.Code)

created, err := linkqueries.QueryLinkMemberships().CreateEach(ctx, db,
    []linkqueries.MembershipDraft{
        draft.SetRole(" ADMIN "),
        draft.SetRole(" VIEWER "),
    })
```

`created` is `[]Membership` in input order. Each row receives its normal UUID preparation, hooks, setters and managed timestamps. The original drafts remain unchanged. Before hooks can supply omitted required fields; final validation still happens through the ordinary model pipeline.

`CreateEach` shares bounded draft preparation with `CreateMany`, but invokes ordinary model writes per input. Existing `CreateMany`, `Upsert` and `UpsertMany` retain their explicit [set-based behavior](model-upserts.md). No row count silently changes whether observers run. A valid empty creation batch returns an empty slice without opening a transaction.

## Updating a selected set

```go
q := linkqueries.QueryLinkMemberships()
f := linkqueries.MembershipFields()

updated, err := q.Where(f.GroupCode.Eq(group.Code)).UpdateEach(ctx, db, 100,
    func(ctx context.Context, tx *database.Tx, current linkqueries.Membership) (linkqueries.MembershipDraft, error) {
        return linkqueries.MembershipDraft{}.
            SetRole("owner-" + current.Role), nil
    })
```

The callback receives a complete stored model and returns its concrete draft. [Lookup writes](model-lookup-writes.md) use the same callback contract when choosing between one existing model and creation. The transaction is the actual operation owner and permits related typed database work after candidate rows have closed. Propagate the supplied context to nested operations. The ordinary update pipeline then locks/reads its current snapshot and runs before hooks, final field preparation, SQL and after hooks. Callback work does not replace normal observer behavior.

The positive maximum is explicit: `100` means the complete matching set must fit within 100 rows. Foundry locks candidates in primary-key order with one lookahead row, closes the stream, checks the bound and validates primary identities before invoking a callback or hook. It reports an error if the set is too large or its primary identities are ambiguous; it never silently truncates a successful batch. The maximum cannot exceed `query.MaxPerModelWriteRows`.

Results follow the selected primary-key order. The original predicates remain in force for each ordinary write. If a callback or earlier hook changes a later candidate so it no longer satisfies those predicates, the resulting failure rolls back the operation. Ordinary read ordering, limits, offsets and eager-loading options are rejected rather than repurposed for writes. Internal candidate and mutation hydration skip `Retrieved` callbacks.

## Deleting and restoring

```go
deleted, err := q.Where(f.GroupCode.Eq(group.Code)).DeleteEach(ctx, db, 100)
restored, err := q.Where(f.GroupCode.Eq(group.Code)).RestoreEach(ctx, db, 100)
removed, err := q.WithTrashed().Where(f.GroupCode.Eq(group.Code)).
    ForceDeleteEach(ctx, db, 100)
```

Each selected model follows its [normal deletion lifecycle](model-soft-deletes.md). `DeleteEach` soft-deletes configured models and physically removes ordinary models. A soft deletion returns the complete stored post-write model; a physical deletion returns its pre-write model. Restoration selects deleted rows while retaining explicit predicates. Force deletion requires soft-delete metadata and retains the chosen active/trashed visibility.

Ordinary deletion changes active rows even with `WithTrashed`; `OnlyTrashed().DeleteEach` therefore selects nothing. Each method validates the complete bounded candidate set before its first model hook. Natural keys retain their declared types, and relation detachment shares this candidate-selection and mutation runtime.

## Atomicity, callbacks and concurrency

One call is atomic within a transaction or savepoint. A later draft callback, hook, codec, constraint or SQL failure rolls back earlier rows and their queued after-commit work. Rows run directly in that one transaction (or the caller's single savepoint), not in a savepoint per row: PostgreSQL caches only 64 subtransaction IDs per backend, and overflowing it slows every concurrent snapshot. Lookup writes and pivot attachment nest their inner write the same way, and factories' `CreateMany` inherits it through `CreateEach`. Cancellation, panic and `runtime.Goexit` use the existing transaction failure handling. A caller-supplied outer transaction still owns final commit, and its locks remain until that commit or rollback.

Larger workloads explicitly choose batches and whether those batches share an outer transaction. The selected row locks protect the current candidate set; they do not claim rows inserted afterward or provide a serializable predicate lock. Database isolation, uniqueness constraints and ordinary conflict/deadlock errors still apply. There is no hidden retry.

After-commit callbacks run only after the outer commit and remain in-process work. They do not provide crash durability. A committed callback failure or uncertain commit outcome retains the candidate slice in the ordinary typed `query.WriteError[[]Model]`; inspect the outcome before retrying. Side effects outside the transaction retain the [normal hook limitations](model-hooks.md).

The [mode tests](../../tests/fixtures/consumer/linkqueries/each_modes_postgres_test.go) cover bulk/per-model separation, hook vetoes, physical deletion and natural keys. The [concurrency test](../../tests/fixtures/consumer/linkqueries/each_concurrency_postgres_test.go) checks that later candidates are already locked before the first callback and that a concurrent later insert stays outside the batch. Compiler-rejection examples retain draft, callback, result and limit types, while real gopls checks expose the generated concrete signatures.

## Set-based mass writes

When per-model behavior is not needed, generated queries also write every matching row in one statement, like Laravel's query-builder `update`, `delete` and `increment`:

```go
q := QueryDocuments().Where(DocumentFields().Published.Eq(false))
updated, err := q.UpdateAll(ctx, db, DocumentDraft{}.SetViews(0))
viewed, err := QueryDocuments().Where(f.ID.Eq(id)).Increment(ctx, db, f.Views.By(1))
archived, err := q.Decrement(ctx, db, f.Views.By(1), DocumentDraft{}.SetTitle("archived"))
deleted, err := q.DeleteAll(ctx, db)             // soft-deletes a soft-delete model
removed, err := q.WithTrashed().ForceDeleteAll(ctx, db)
```

Each returns the affected row count. The statement uses the query's filters, [global scopes](model-global-scopes.md) and soft-delete visibility; ordering, pagination and eager-loading options are rejected. `UpdateAll` applies managed update timestamps and field mutators once. `Increment`/`Decrement` take a typed `query.Adjustment` from a numeric generated field (`Field.By(delta)`), compile to `column = column + $1`, leave SQL NULL as NULL, and accept one optional extra draft. `DeleteAll` soft-deletes active rows of a soft-delete model and physically deletes ordinary rows; `ForceDeleteAll` (soft-delete models only) physically deletes the rows the current visibility selects. The [tenant consumer](../../tests/fixtures/consumer/tenantqueries/tenant_postgres_test.go) exercises them against PostgreSQL.

These writes never hydrate models, so per-model hooks, provider observers and change capture do not run. On a model that declares hooks, or when the executor cannot prove that no observers are registered, they fail with `fault.Invalid` unless the query acknowledges this with `WithoutModelHooks()`. The acknowledgement affects only set-based writes; per-model writes on such a query are rejected so it cannot be mistaken for disabling hooks. Use the `Each` methods above when every row needs its lifecycle.
