# Typed datatables and exports

Milestone 19 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-19-verification-and-consumer-review)
records the checks and operational limits.

## Declare one row and its columns

Use an explicit response projection:

```go
//foundry:projection dto=true
type MemberRow struct {
    ID       model.ID[Member]       `json:"id"`
    Name     string                 `json:"name"`
    Nickname value.Nullable[string] `json:"nickname"`
    Balance  decimal.Decimal        `json:"balance"`
}
```

The opt-in adds the ordinary DTO JSON descriptor and validation field selectors
to the projection's existing generated file. `MemberRowJSON()` owns its wire
graph; `MemberRowValidationFields()` owns JSON names and Go getters;
`MemberRowProjection()` owns SQL output mapping. An ordinary projection does not
become a public DTO. Persistence models, persistence tags and unsupported custom
codecs remain rejected at DTO boundaries. Use JSON tags for public property names
and typed projection mappings for the selected SQL expressions.

`datatable.DefineColumn[Source](wire.Name, label)` retains source scope, response
row and field value types. Add `SortBy(expression)`, `FilterBy(source)`,
`Searchable()` and `ExportAs(cell)` only for supported capabilities, then call
`Registration()`. The table checks each column against a scalar property in the
row's existing JSON graph, including enum cases, widths, nullability and formats.
Arrays, objects and dynamic fields cannot be scalar table columns.

Labels use `i18n.MessageKey`, independent of the locale. Keys use the shared
semantic identifier grammar; constructing a key does not prove catalog
membership. The manager borrows a locale catalog and heading resolver for exports.
The [localization guide](localization.md) covers UI catalogs and locale resolution.

The [consumer member table](../../tests/fixtures/consumer/reporting/members.go)
combines a computed name, enum/nullable/exact-decimal columns and a declared
relation filter. [Joined and grouped tables](../../tests/fixtures/consumer/reporting/orders.go)
reuse the same column API.

## Authorization and server scope

`datatable.Spec[S, R, A]` fixes the SQL scope, response type and trusted server
authority. `Authorize(ctx, authority, action)` and `Source(ctx, authority)` are
mandatory. Actions distinguish query, export and inspection. Public/system
reports still declare an explicit authorization callback.

The source supplies mandatory tenant, visibility and soft-delete restrictions.
Client predicates are added outside that scope. List, count and export all use
the same preparation and source pipeline. A request cannot replace the scope or
select arbitrary SQL names. Invalid names, operators and scalar values fail
before execution. Authorization runs before source construction, locale/heading
callbacks, temporary files and database work.

An interactive service can pass an `auth.Guard[M]` and invoke a registered typed
policy in its authorization callback. The consumer's private
[Authority](../../tests/fixtures/consumer/reporting/authority.go) supports both
authenticated requests and freshly resolved job actors. It shares the domain
permission decision and never accepts roles or authority in request JSON.

Managers require the exact registered table declaration. A second declaration
with the same ID cannot substitute a different authorization callback. Registry
construction rejects duplicate IDs. `Table.Description()` is trusted assembly
metadata; use `Inspect(ctx, manager, authority)` when exposing it to clients.

## Requests, filtering and deterministic pages

Decode with `datatable.DecodeRequest(ctx, body)` or the generated
`datatable.RequestJSON()` transport contract. The bounded shared decoder rejects
unknown/duplicate JSON keys, invalid Unicode and wrong scalar representations.
For example:

```json
{
  "page": 1,
  "size": 20,
  "sort": [{"column": "name", "direction": "asc"}],
  "filters": [{"op": "between", "column": "balance", "values": ["1.25", "50"]}],
  "search": "Ada"
}
```

Zero/omitted page and size default to 1 and 20. Other invalid values are rejected,
not clamped. Sort names and directions use the declaration allowlist; repeated
sort columns fail. Client sorts replace the default sort, then the table appends
its mandatory `Stable` orders. The author must supply a unique final key for the
actual result, including joined or grouped rows.

The source must be unpaginated. `ReorderUnwindowed` replaces outer ordering and
rejects outer LIMIT/OFFSET and DISTINCT ON because they change result selection.
Materialize a DISTINCT ON result before reporting on its completed rows. Inner
windows and winner ordering remain intact. Ordinary DISTINCT stays intact.

Use `Where(codec, field)` or `Having(codec, aggregate)`; nullable counterparts
retain the exact nullable column type while parsing non-null values with the
same codec. Available comparisons come from that typed expression. `Restrict`
narrows the operator allowlist. `Related` maps an explicitly declared child row
filter through a typed relation predicate. Extra filters need not be displayed
columns, but must have distinct declared names and labels.

Values are scalar text, including integers and decimals. Empty string remains a
string; SQL NULL uses `is_null`/`is_not_null`, without values. Boolean trees use
`and`, `or` and single-child `not`. AND may contain WHERE and HAVING filters;
mixed-phase OR/NOT fail because moving them between phases would change meaning.

Global search ORs only declared searchable columns. It prefers `icontains` when
the source supports it, otherwise literal `contains`. PostgreSQL owns case
folding. `%`, `_` and `!` stay literal for these substring operators; `like`
permits SQL pattern syntax only when explicitly allowed. Search across mixed
WHERE/HAVING phases is rejected.

`table.Query(ctx, manager, authority, request)` returns `query.Page[R]` using one
count and one row query in a read-only repeatable-read transaction.
`table.Count(...)` applies the same filters and authorization with one count query.
Pagination is validated for both; count represents the complete matching result.
Failed queries return no partial page. Direct Go callers retain ownership of
request slices until the operation returns.

## Complete, bounded export artifacts

Enable exports in the spec and mark exportable columns with `ScalarCell(codec)`
or `NullableCell(codec)`. These reuse the scalar codec for exact value formatting;
NULL renders as an empty cell. `FormatWith` supplies explicit human presentation
without changing the JSON/filter contract. The callback receives the validated
locale and time zone. Locale defaults to the catalog default; time zone defaults
to `Config.TimeZone`, inherited from the [application timezone](application-timezone.md)
in configured assembly or UTC for a direct manager. Explicit per-export
`Presentation.TimeZone` wins. Formatters must remain bounded and honor cancellation.

```go
artifact, err := table.Export(ctx, manager, authority, request, datatable.ExportOptions{
    Format: datatable.CSV,
    Name: "members.csv",
    Presentation: datatable.Presentation{Locale: "en", TimeZone: "Asia/Kuala_Lumpur"},
})
if err != nil { return err }
defer artifact.Close()
// Read/Seek, Name, MediaType, Size, Rows and SHA256 describe the completed file.
```

Export validates page settings but exports **all matching rows**, bounded by
`MaxExportRows`. It streams one query into a private temporary file and returns
an artifact only after row decoding, formatting, CSV/ZIP finalization and seek
succeed. A lookahead row rejects oversized reports instead of silently truncating.
The filename is normalized as a display name and receives the selected extension.
It never controls a filesystem path or storage key.

CSV uses standard delimiter/newline/quote escaping and prefixes potentially
interpreted formula text with an apostrophe: `=`, `+`, `-`, `@`, including after
leading Unicode whitespace, and leading tab/CR. This changes the literal CSV
value, including negative numeric text. Re-saving/re-importing CSV can change a
spreadsheet's interpretation; consumers must retain a safe import policy.

XLSX uses one worksheet and inline-string cells. Formulas, macros, hyperlinks,
external links and a growing shared-string table are absent. Exact integers and
decimals remain text, so spreadsheet numeric precision cannot round them. XML
escaping and OOXML literal-escape handling preserve text and carriage returns.
The ZIP has a fixed number of parts and streams worksheet rows. Both compressed
file bytes and uncompressed XML bytes are bounded; there is no workbook-sized
memory buffer. These are resource bounds, not a throughput guarantee.

The artifact retains export capacity until `Close`. Cancellation invalidates
reads but does not abandon an executing callback or open file. Close is serialized
and idempotent. Failed file removals remain manager-owned, prevent new exports,
and are retried by Close, the next export and manager shutdown. A failed export
never publishes a partial artifact. Retain the manager if shutdown reports a
cleanup failure so removal can be retried after the underlying filesystem issue
is repaired. Applications should use a private, application-owned temporary
directory and normal operational cleanup for process-crash leftovers.

Defaults include 32 query operations, 2 exports, one-minute query and ten-minute
export timeouts, page size at most 1000, offset at most 1,000,000, 50,000 export
rows, 64 MiB file bytes and 256 MiB XML. Individual encoded rows are limited to
256 KiB and their page sum to 4 MiB. Cells are limited to 64 KiB and 32,767 UTF-16
units. Requests have a 64 KiB encoded limit, 128 filter nodes, bounded nesting,
100 values per filter, 4096 bytes per scalar/search and 8 requested sort fields.
`DefaultConfig` is the runtime source of these defaults; `Validate` checks custom
limits before any I/O.

## HTTP, jobs and lifecycle

`table.Download(...)` freezes a validated request and returns the existing typed
HTTP download. Opening it runs export authorization and scoping again. The
HTTP file response owns the artifact, ranges and cleanup. Its own file-response
limits still apply. See the [consumer endpoint](../../tests/fixtures/consumer/reporting/http.go)
and [download guide](http-downloads.md).

`datatable.ExportHandler` adapts a table to an ordinary `jobs.Handler[P]`.
The resolver must reload current authority from a trusted queued reference; the
table then applies export authorization again. Delivery receives a completed
artifact and must finish reading before returning. Its context retains the job's
identity, the export deadline and ownership tracking; closing the same manager
from delivery fails with a cycle error. Cancellation cannot report success even
if the delivery callback returns nil. The handler closes the artifact on
success, failure, panic and Goexit. Jobs remain at-least-once; use the definition's
`CurrentID(ctx)` as an idempotent destination identity. The
[consumer declaration](../../tests/fixtures/consumer/reporting/export_job.go)
stores typed actor/request/presentation data, with no captured role claims.
Use the existing [jobs/outbox](jobs.md) path for durable transactional dispatch.

`datatable.Module` borrows database/locale dependencies and preserves their
lifetime until query callbacks and export owners actually exit. Export workers
must depend on this module and shut down first. `Manager.Close(ctx)` cancels both
operation groups; the context bounds the caller's wait. It does not force-close
an artifact still owned by another caller. Separate `DoneQueries`/`DoneExports`
signals report actual exit. Self-shutdown from an active table callback fails
before either group is partially canceled.

`Registry.Descriptions()` contributes row/request schemas, labels, capability
allowlists and typed scalar metadata to the shared manifest. The [TypeScript emitter](client-contracts.md) consumes that shared manifest;
this package does not infer a second client schema.
