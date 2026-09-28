# Update and delete using typed query sources

Generated source-write builders preserve the destination model, source scope, stored field types and draft input types. They support typed joins, aliases, CTEs, predicates and explicit source windows through the same SELECT compiler as reads. These are set-based operations: Foundry does not hydrate and dispatch observers for every candidate.

## Copy stored values from a query

For the independent consumer's `Archive` and `Member` models:

```go
a := linkqueries.ArchiveFields()
m := linkqueries.MemberFields()

updated, err := linkqueries.UpdateMemberFrom(
    linkqueries.QueryLinkMembers().Where(m.Name.Ne("protected")),
    linkqueries.QueryLinkArchives().Where(a.Tag.Eq("approved")),
).
    MatchID(a.MemberID.Value()).
    SelectName(a.Name.Value()).
    SelectAlias(a.Alias.Value()).
    Returning(ctx, db, 100)
```

`MatchID` matches a source expression to the destination's model-owned primary ID. A natural primary named `Code` has `MatchCode`. Matching never assigns the primary key. A nullable key from an outer join requires `MatchNullableID` or the corresponding natural-key method; SQL NULL matches nothing.

`SelectAlias` preserves its nullable stored type. Selectors read stored SQL values; they do not call Go getters. Automatic field documentation points to declared getters and setters so callers can choose the correct domain/DTO behavior.

The destination query retains its predicates and soft-delete visibility. Apply ordering or limits to the source; destination writes reject pagination and eager-loading options. Source selection is not silently truncated by a result limit.

## Fixed draft inputs and setters

```go
fields := linkqueries.MembershipFields()
count, err := linkqueries.UpdateMembershipFrom(
    linkqueries.QueryLinkMemberships(),
    linkqueries.QueryLinkMemberships().Where(fields.Role.Eq("reader")),
).
    MatchID(fields.ID.Value()).
    Values(linkqueries.MembershipDraft{}.SetRole(" EDITOR ")).
    Exec(ctx, db)
```

`Values` replaces fixed inputs applied to every affected model. Ordinary setters normalize each supplied input once per attempt inside the transaction; managed timestamps use that transaction's application clock. A distinct setter input retains its declared Go type. The caller's draft remains unchanged.

A field cannot appear in both `Select<Field>` and `Values`, repeat, or change the primary key. Go-mutated fields and managed update timestamps have no SQL selector; the runtime declaration boundary also rejects those bypasses. Explicit setters cannot execute once per SQL-selected row. Use the [per-model write APIs](model-batch-writes.md) when domain behavior must run for each model.

## Duplicate matches and outcomes

[PostgreSQL UPDATE FROM](https://www.postgresql.org/docs/18/sql-update.html) can otherwise select an arbitrary source row when several rows match one destination. Foundry rejects an SQL-mapped update when any affected model has multiple source matches, returning `database.TooManyRows` and rolling back the write. It does not choose a hidden first row, retry, or infer that duplicate values are equivalent.

Foundry materializes the complete source window, matches it using the destination's actual database key comparison, and counts matches by destination key. `Exec` checks two aggregate scalars inside the owning transaction; it does not collect affected models. Destination-excluded or trigger-suppressed models do not contribute to affected counts or ambiguity errors.

Updates with only fixed inputs and timestamps permit repeated source matches: every affected model receives the same inputs once. Source deletes also affect each matching model once.

`Returning` collects at most its explicit bound, from 1 through `query.MaxInsertRows`. Exceeding the bound rolls back rather than committing a partial selection. This bounds retained models, not database work or individual field sizes, and does not promise result order. Empty matches return zero or an empty slice. Write failures discard results; uncertain commit confirmation retains reconciliation data through the ordinary typed `query.WriteError` contract.

## Delete using a source

```go
fields := linkqueries.MembershipFields()
count, err := linkqueries.DeleteGroupUsing(
    linkqueries.QueryLinkGroups(),
    linkqueries.QueryLinkMemberships().Where(fields.Role.Eq("obsolete")),
).MatchCode(fields.GroupCode.Value()).Exec(ctx, db)
```

Ordinary removal soft-deletes configured models, including the normal active-only protection and clock conventions. `ForceDelete<Model>Using` exists only for soft-delete models and physically deletes within the destination's explicit visibility; `WithTrashed` includes already deleted records. A model without soft deletion uses physical [DELETE USING](https://www.postgresql.org/docs/18/sql-delete.html). Physical deletion's `Returning` provides complete old models.

These APIs skip per-model write and retrieval hooks. They preserve normal transaction/savepoint ownership and cancellation. Database constraints, triggers and authorization scopes remain runtime concerns. No cross-system durability or per-model audit dispatch is implied by a set-based statement.
