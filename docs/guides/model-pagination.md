# Typed query pagination

Generated model queries expose numbered, count-free simple and model-owned cursor pages. All keep complete typed models, preserve query filters/orderings, and append primary-key ascending order when the key is absent. The [independent consumer](../../tests/fixtures/consumer/model_pagination_postgres_test.go) verifies natural keys, tied values and backward traversal. [Projection, set and value pages](../../tests/fixtures/consumer/pagequeries/pages_postgres_test.go) reuse the same request and result containers with explicit ordering.

## Numbered pages

This is the database counterpart of Laravel's `paginate`: Foundry computes the offset, matching total, page count and typed items. Automatic HTTP request parsing and response links are planned in [milestone 08](../../blueprint/08-http-validation-and-responses.md#automatic-pagination-at-the-http-boundary). The same database API remains usable from CLI and worker code.

```go
page, err := models.QueryUsers().
    Where(models.UserFields().Age.Gte(18)).
    Paginate(ctx, db, query.PageRequest{Number: 1, Size: 20})
// page.Items is []models.User
```

`Page[models.User]` contains `Items`, `Number`, `Size`, `Total` and `Pages`. Numbers start at one. Empty datasets report zero total/pages and an empty collection. Requests past the last page retain the requested number and return an empty collection. Size must be between one and `query.MaxPageSize`; invalid sizes, page numbers and offset overflow fail before execution. Limits are rejected rather than clamped silently.

The total counts every row matching the base filters. It is a separate SQL query from the page read. For a shared snapshot, execute both through a caller-owned repeatable-read transaction. Separate requests do not share that snapshot. Large offsets still require PostgreSQL to process the skipped rows; cursor pagination is appropriate when navigating large datasets without totals. [PostgreSQL LIMIT/OFFSET](https://www.postgresql.org/docs/18/queries-limit.html)

## Simple pages without a total

```go
page, err := models.QueryUsers().
    Where(models.UserFields().Age.Gte(18)).
    SimplePaginate(ctx, db, query.PageRequest{Number: 1, Size: 20})
// page.Items is []models.User; page.HasMore reports another row after this page.
```

`SimplePage[Result]` contains `Items`, `Number`, `Size` and `HasMore`. It deliberately has no total or page-count fields. This supplies the count-free behavior of [Laravel simple pagination](https://laravel.com/framework/docs/13.x/pagination#simple-pagination). Size/number validation and offset overflow checks reuse `PageRequest`; model ordering uses the same primary-key tie-breaker as numbered pages.

One base query reads at most size plus one rows. The extra row is fully decoded, then cleared and removed before returning the slice. It determines `HasMore`; an exactly full last page has `HasMore == false`. Empty and beyond-last pages return empty items and false. A number greater than one permits requesting the preceding page, without asserting that page still contains rows. There is no count query, but OFFSET still incurs skipped-row work.

Model simple and cursor pages load requested relations only for returned models. The hidden extra row does not trigger relation queries or consume the related-row budget. Eager loading may add its normal batched queries, and its failures discard the complete page. Invalid stored values in the extra row still fail decoding.

## Projected, combined and single-value results

```go
u := models.UserFields()
report := reports.SelectUserSummary(models.QueryUsers(),
    reports.UserSummarySelection[models.User]{
        ID: u.ID.Value(), Email: u.Email.Value(),
        Nickname: u.Nickname.Value(), Status: u.Status.Value(),
    })
page, err := report.OrderBy(u.Email.Asc(), u.ID.Asc()).
    Paginate(ctx, db, query.PageRequest{Number: 1, Size: 20})
// page.Items is []reports.UserSummary
```

`ProjectionQuery`, `SetQuery`, `ValueQuery` and `ValueSetQuery` expose `Paginate` and `SimplePaginate`. `Page[Result]`/`SimplePage[Result]` retain the complete declared result or selected value, including nullable wrappers and exact decimal codecs. A page of summaries cannot be assigned to a page of persisted users.

These read-only queries require an explicit ordering at the level being paginated. Foundry cannot infer a unique key for arbitrary grouped rows, joins or reports. Supply all keys needed for a deterministic order; ordering presence can be validated, but uniqueness remains the query/schema contract. Complete-model projections and distinct model selections follow this same rule. Ordering a nested input alone does not order its outer result. [PostgreSQL LIMIT/OFFSET](https://www.postgresql.org/docs/18/queries-limit.html)

The total counts complete result rows after filters, grouping/HAVING, distinctness and set operations. Joined duplicates remain separate unless deduplicated explicitly. Count preserves the complete selection and ordering, including ordering that chooses `DistinctOn` winners. A numbered page executes a count and a separate bounded read even when the count is zero; use a repeatable-read transaction when both must share a snapshot.

An existing limit or nonzero offset on the level being paginated is rejected. Deliberate limits inside derived inputs, CTEs and individual set operands remain intact. Window expressions evaluate before the new page limit/offset. The complete page query is validated before a count can reach the executor. [Result cursors](result-cursor-pagination.md) wrap a completed projection, set, CTE or value query with typed output ordering and an explicit unique identity; model cursors are available below.

## Cursor pages

```go
q := models.QueryWriteRecords().OrderBy(models.WriteRecordFields().Note.Asc())
page, err := q.CursorPaginate(ctx, db,
    query.CursorRequest[models.WriteRecord]{Size: 20})
```

`CursorPage[models.WriteRecord]` contains complete `Items`, `Size`, and optional model-owned `Next`/`Previous` cursors. To continue forward, pass the returned next cursor as `After: value.Set(next)`. To move backward, pass the previous cursor as `Before: value.Set(previous)`. Omit both for the first page; providing both is invalid. Page size may change between requests.

`CursorPaginate` performs one query for size plus one rows, with no count. The extra row indicates whether more rows exist in the requested direction. Returned items always follow the declared order, including when moving backward. The opposite navigation cursor uses the first/last returned position and the supplied boundary; it is a navigation hint and does not separately prove a row still exists on that side. Empty results have no navigation cursors.

Multiple sort columns may mix ascending and descending directions. The primary key resolves ties. Nullable fields follow PostgreSQL defaults: NULLs last for ascending, first for descending. Foundry compiles explicit null-aware lexicographic predicates through the shared query AST, reverses ordering when fetching backward, and restores canonical order for the result. When every sort field is declared NOT NULL and all share one direction, the boundary is a single row comparison such as `("created_at", "id") < ($1, $2)`, which a composite index on the same columns in the same direction serves as one range; nullable fields keep the expanded NULL-aware predicate, and NOT NULL fields never add an `IS NULL` alternative. Explicit `NullsFirst`/`NullsLast` placement is rejected for cursor ordering. [PostgreSQL ordering](https://www.postgresql.org/docs/18/queries-order.html)

A cursor binds to the query's filters and their bound values. A filter relative to "now" computed in Go (for example `f.CreatedAt.Gt(now.Add(-24 * time.Hour))`) binds a different value on every request, so the next request's query no longer matches the cursor and is rejected. Keep the relative window in SQL instead, so the bound values stay constant across pages:

```go
elapsed, err := temporal.Elapsed(24 * time.Hour)
if err != nil {
    return err
}
since := query.SubtractInstantInterval(query.TransactionTime(q), elapsed, query.UTCZone())
page, err := q.Where(query.Greater(f.CreatedAt, since)).CursorPaginate(ctx, db, request)
```

`TransactionTime` uses the database clock at each request's transaction start, so the window moves between pages; rows that age out of it disappear from later pages, as they would for any live filter. When every page must see the same window, choose the boundary once and send that same value with every page request instead.

At most `query.MaxCursorFields` sort fields, including the tie-breaker, are accepted. All model pagination methods reject a preexisting `Limit` or nonzero `Offset`, and repeated ordered columns. They never silently discard those clauses. Ordinary `Limit`/`Offset` reads remain available separately.

## Typed transport boundary

Use `cursor.Token()` when explicitly exporting a cursor to a transport. `query.ParseCursor[models.WriteRecord](token)` validates its bounded, versioned structure. Query execution then verifies its fingerprint and decodes its keys through generated field codecs. It rejects a different model, selected columns, filters/bindings or ordering. This describes the query itself; connection/session state, authorization policies and database identity are not encoded in that fingerprint.

Cursor types and cursor requests retain their model owner in Go. A cursor or request for an order cannot be passed to a user query. A string arriving over HTTP has no compile-time provenance, so parsing and query validation remain runtime checks. The token is an implementation-owned format; clients should store and return it unchanged.

Tokens contain encoded sort values and are neither encrypted nor signed. They provide position, not permission. Apply authorization and tenant scopes on every request, and expose only sort values appropriate for that transport. Ordinary formatting redacts the token; `Token()` is the explicit disclosure boundary. Models and page containers do not automatically become public response DTOs.

Input tokens are bounded by `query.MaxCursorBytes` before decoding. Oversized output tokens fail the entire page; they are never truncated into a different boundary. Keys preserve integer precision, exact decimal strings, nullable state, UUIDs and temporal values. Invalid enums, overflow and malformed keys fail their codecs before SQL. Generation emits typed getters and reuses the same field-codec selection as reads/writes; no runtime field-name lookup or reflection over model fields is required in consumer code.

## Consistency and errors

A stable keyset position prevents an insertion before that position from shifting the next page, but it is not a durable snapshot. Concurrent deletion, changes to sorted fields, collation changes or authorization changes can alter later pages. Use stable sort fields and indexes matching actual filters/orderings. Database collation and schema semantics remain authoritative.

Any query, count, row-decode, lookahead-decode, eager-load, cleanup or token-generation failure returns a zero page, discarding all partial items and metadata. Context cancellation propagates through the database runtime. Returned row count is bounded by page size, with one additional decoded row for simple/cursor lookahead; individual row/field sizes remain separate database/adapter constraints.

## Verification

Unit and protocol tests cover offset overflow, ambiguity rejection, bound SQL, scope mismatches, typed cursor decoding, cancellation, count errors and partial-result disposal. Targeted fuzzing exercises the bounded token parser. Real PostgreSQL consumer tests cover page totals, empty/beyond-last pages, natural keys, ties, nullable/mixed-direction sorts, exact decimals, temporal/UUID boundaries, forward/backward traversal, changed query/model rejection and insertion before/after an existing boundary. Compiler assertions reject incompatible cursor owners/requests and partial response records used as write drafts. gopls exposes pagination alongside the generated read/write methods.

Simple/projection acceptance additionally checks query counts, exactly full last pages, eager lookahead exclusion, failed related reads, malformed lookahead records, grouped/distinct/join/set totals, nested CTE/set limits, window evaluation and complete result types. Unit checks verify immutable windows, cleared lookahead references, count overflow and failure ordering. Negative compilation checks retain model/projection ownership and nullable result types, and reject totals on simple pages.
