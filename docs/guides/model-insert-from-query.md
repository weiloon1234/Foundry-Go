# Insert models from a typed query

`Insert<Model>From(source)` inserts a query's selected SQL values into another model. The generated builder retains both the source scope and destination field types. It executes one set-based statement through Foundry's existing query compiler and transaction runtime.

The independent [consumer fixture](../../tests/fixtures/consumer/linkqueries/insert_select_postgres_test.go) exercises these examples against PostgreSQL. This API is part of the framework; the fixture is not a starter application.

## Select stored values and supply literal inputs

The fixture's `Archive` stores a typed `MemberID`, a name and nullable alias from `Member`. It also has a custom `MutateTag(ArchiveTagInput) (string, error)` setter, exact decimal amount, typed JSON payload, managed timestamps, and database-owned defaults for its own ID and counter.

```go
fields := linkqueries.MemberFields()
source := linkqueries.QueryLinkMembers().
    Where(fields.Name.Like("team%"))

insert := linkqueries.InsertArchiveFrom(source).
    SelectMemberID(fields.ID.Value()).
    SelectName(fields.Name.Value()).
    SelectAlias(fields.Alias.Value()).
    Values(linkqueries.ArchiveDraft{}.
        SetTag(linkqueries.ArchiveTagInput{Text: " IMPORT "}).
        SetAmount(amount).
        SetPayload(payload))

archives, err := insert.Returning(ctx, db, 100)
```

`archives` is `[]linkqueries.Archive`, with complete model hydration. A member ID cannot substitute for an archive ID. A nullable expression cannot substitute for a required string. Expressions from a different source scope also fail compilation. `query.Nullable(expression)` explicitly widens a non-null expression when the destination is nullable.

`Select<Field>` supplies a stored SQL expression. It may use the existing typed calculations, subqueries, joins or aliased CTE fields that belong to the source scope. It does not run a Go getter. Generated field comments identify custom getters so the caller can choose stored values for SQL and getter results when preparing a response DTO.

`Values(draft)` supplies fixed input values for every selected row. It replaces the previous literal draft without changing the original builder. A column must have one source: selecting it twice or supplying it through both a selector and the draft fails validation. Selectors append; they do not silently replace each other.

## Setters, timestamps and omitted fields

Literal draft inputs pass through their ordinary Go setters once inside the write transaction. Nullable NULL stays NULL, omitted input stays omitted, and setter failure rolls back the operation. A distinct setter input type remains required, as in `ArchiveTagInput` above.

Generated selectors are absent for columns with Go setters. The runtime rejects manually constructed mappings to these columns too. Arbitrary SQL results cannot invoke an application Go function per row; use bounded source rows and `CreateEach` when each source value needs a setter or model lifecycle hooks.

Managed `CreatedAt` is supplied from the owning application clock when neither mapped nor explicitly assigned. An explicit creation value is preserved. Managed `UpdatedAt` uses the same clock sample, passes through its field mutator, and has no SQL selector. The ordinary timestamp opt-out restores explicit field ownership.

Omitted nullable columns and declared database defaults remain owned by PostgreSQL. Required columns without defaults must be supplied. An omitted UUID primary key needs a real migration-owned default; insertion does not allocate one Go UUID and reuse it for every selected row. A fixed ID in `Values` is a fixed ID, so multiple selected rows can fail the primary-key constraint. A query insertion with no supplied columns is rejected; PostgreSQL does not provide a repeated `DEFAULT VALUES` select form.

## Choose the terminal result

`insert.Exec(ctx, writer)` returns an `int64` affected-row count without collecting models. It processes the source's complete selected window and is not capped by `MaxInsertRows`.

`insert.Returning(ctx, writer, limit)` collects complete models, with an explicit limit from 1 through `query.MaxInsertRows`. More returned rows produce `database.TooManyRows` and roll back the entire insertion. Results are discarded on ordinary failure. The limit bounds retained models, not database work or each model's field sizes; use source filters and pagination to constrain the selected work.

Returning never changes the source's limit or offset. In particular, it cannot silently skip later source rows because an earlier row was suppressed by a database trigger. PostgreSQL's returning order is not a mapping to source order. An empty source returns an empty result or an affected count of zero; its literal inputs are still validated and prepared.

## Transaction and lifecycle behavior

This explicit set-based operation skips per-model write observers and retrieval hooks. Source soft-delete visibility, predicates, ordering and windows remain effective; aliased CTEs retain their declared scope. It does not synchronize or modify the schema.

Execution joins the existing transaction through a savepoint or opens its own transaction. Setter failure, constraint failure, cancellation and excessive returned rows preserve ordinary rollback behavior. A reusable parent transaction remains usable after an isolated failure. There are no hidden retries.

An uncertain commit or failed after-commit callback uses the shared `query.WriteError[R]` reconciliation contract. `R` is `int64` for `Exec` and `[]Model` for `Returning`; its candidate is not proof of persistence. Routine builder formatting omits captured parameters and draft inputs.

See [model batch writes](model-batch-writes.md) for observer-aware alternatives, [field transformations](model-mutators.md) for setter/getter ownership, and [model writes](model-writes.md) for transaction outcomes.
