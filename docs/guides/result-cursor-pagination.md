# Cursor pagination for complete results

`CursorFor` pages a complete projection, model selection, CTE or set through its typed output fields. `ValueCursorFor` handles single-value queries and value sets. Both return the existing `CursorPage[Result]`, whose `Items` is an ordinary Go slice. The [consumer fixture](../../tests/fixtures/consumer/cursorqueries/cursors_postgres_test.go) demonstrates these APIs with generated models and reports.

## Declare the result identity

```go
u := models.UserFields()
report := reports.SelectUserSummary(models.QueryUsers(),
    reports.UserSummarySelection[models.User]{
        ID: u.ID.Value(), Email: u.Email.Value(),
        Nickname: u.Nickname.Value(), Status: u.Status.Value(),
    })
cursor := query.CursorFor(report)
fields := reports.UserSummaryFieldsAt(cursor.Scope())
ordered := cursor.OrderBy(fields.Nickname.Asc()).UniqueBy(fields.ID.Group())
page, err := ordered.Paginate(ctx, db,
    query.CursorRequest[reports.UserSummary]{Size: 20})
// page.Items is []reports.UserSummary.
```

`Scope()` identifies the completed output as `CursorScope[Result]`. Generated `FieldsAt` functions retain each field's result owner, value type and nullability. Input model predicates and ordering descriptors cannot be used as output descriptors. Select a computed sort expression into a declared result field first, then order that output field.

`UniqueBy` is required. Its nonempty combination of output fields must identify every result row under the database's equality/collation semantics, treating NULLs as equal. It is an application assertion: Foundry does not deduplicate rows, create a unique constraint or query the whole dataset to prove it. In particular, a nullable SQL unique constraint that allows repeated NULLs is insufficient by itself. Missing, repeated, unknown or unavailable fields fail validation before execution.

Identity fields absent from `OrderBy` are appended ascending. An explicitly ordered identity field keeps its direction. Calling `UniqueBy` again replaces the identity declaration. Calling `OrderBy` appends ordering; repeated fields are rejected. At most `query.MaxCursorFields` distinct ordered fields, including appended identity fields, are permitted.

A selected model ID is not always a result identity. Joining users to orders repeats each buyer; a complete report can declare `UniqueBy(fields.BuyerID.Group(), fields.OrderID.Group())`. Grouped reports can use their complete grouping key. If result rows are indistinguishable by every selected field, select the missing identity fields or explicitly deduplicate first. A wrong uniqueness assertion can skip rows between pages.

## Preserve the complete source query

The cursor adds a derived-result boundary around its source. Inner filters, joins, grouping/HAVING, window functions, distinct selection, CTEs, set operations, limits and offsets remain inside it. The cursor predicate and page limit apply outside that completed result. For example, a cursor sorted by an aggregate filters completed groups; a selected row number keeps its original value on later pages.

The outer order is explicit and independent of an inner order. An inner order may still choose `DistinctOn` winners or a bounded input dataset. Calling `CursorFor(report.Limit(100))` intentionally pages only that bounded input; specify a deterministic inner order when choosing those 100 rows. Existing model `CursorPaginate` instead operates at the model query level and rejects an existing limit/offset. See [model pagination](model-pagination.md).

`cursor.Where(fields.Email.Contains("example"))` filters completed output rows. Domain or authorization filters that belong to input models go on the source query before `CursorFor`. No filter is inferred from the presence of a cursor token.

## Nullable values and navigation

```go
values := query.SelectValue(models.QueryUsers(), u.Nickname.Value()).Distinct()
cursor := query.ValueCursorFor(values)
ordered := cursor.OrderBy(cursor.Value().Asc()).UniqueBy(cursor.Key())
page, err := ordered.Paginate(ctx, db,
    query.CursorRequest[value.Nullable[string]]{Size: 20})
```

`Value()` retains the exact scalar codec; `Key()` identifies its output column. `UniqueBy(cursor.Key())` asserts that values are unique. The explicit `Distinct()` above removes duplicates, including repeated NULLs. No deduplication is implicit in the cursor builder.

Navigation reuses the model cursor machinery: omit both boundaries to start, pass `After: page.Next` to continue, or `Before: page.Previous` to go backward. Only submit a returned optional cursor when it is set; an absent boundary means start from the beginning. Size may change between requests. PostgreSQL's default NULL ordering applies: ASC puts NULL last, DESC first. Mixed directions and nullable keys use explicit lexicographic predicates; going beyond an all-NULL final ascending key yields an empty page. Backward reads restore the canonical order of the returned slice. [PostgreSQL ordering](https://www.postgresql.org/docs/18/queries-order.html)

Each page executes one query for at most size plus one complete rows, without a total query. The extra row supplies the forward/backward continuation hint. An exactly full last page has no further cursor in that direction. The opposite cursor is a position hint, not a second existence check. Empty pages contain no navigation cursors. Eager-loading source options are rejected here; use the ordinary model `CursorPaginate` when relationships should load alongside model pages.

## Tokens, failures and ownership

`Cursor[Result]`, `CursorRequest[Result]` and `CursorPage[Result]` preserve the result type. Parsing a transport token uses `query.ParseCursor[Result]`; execution additionally validates the result/query fingerprint and every ordered field through its generated codec. The fingerprint includes the complete source SQL and bound values, outer filters/order, and the unique-key declaration. Changing the page size does not change that identity. Connection/database/session identity and authorization state are not part of the fingerprint.

Tokens use the shared bounded format and contain encoded sort values; they are neither signed nor encrypted credentials. Use `Token()` only at an explicit transport boundary. Apply authorization on every request. Query changes, concurrent writes and sort-key changes do not provide a snapshot across requests. The [shared cursor contract](model-pagination.md#typed-transport-boundary) owns token bounds, confidentiality limits and consistency semantics.

Generated projections now provide typed field getters alongside their existing complete decoder. Aliases, record selections, ordinary/recursive CTEs and sets carry that metadata. Handwritten `DefineProjection` declarations can still omit getters for ordinary reads; cursors require `NewRecordField` metadata for every ordered field. Generated declarations reuse the same codec source used by hydration.

Query, row decoding, lookahead decoding, cleanup and token-generation errors return a zero page. Rows close before the page is returned, including on oversized tokens and malformed lookahead records. The row count is bounded; individual field sizes remain separate database/adapter constraints. Cursor builders expose no mutation terminals and cannot be reused as unrestricted query sources.

## Slices after fetching

Results remain idiomatic Go slices. Sorting a copy in memory is explicit:

```go
items := slices.Clone(page.Items)
slices.SortFunc(items, func(a, b reports.UserSummary) int {
    return cmp.Compare(a.Email, b.Email)
})
```

This uses the standard [slices helpers](https://pkg.go.dev/slices#SortFunc) and `cmp` package. It sorts only the fetched items. Keep the original page ordering and cursors together for database navigation; use `OrderBy` when sorting should apply across the full dataset. A separate collection wrapper is not required for these operations.
