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

Rejected table request names, operators, values and bounds return an error matching
`http.BadRequest` and `fault.Invalid`. Return that error unchanged from a handler:
HTTP sends the existing localized `bad_request` response (400) without private
error text. The same classification applies to malformed `DecodeRequest` input
and invalid export format/presentation options. Decoding retains the underlying
`contract.DecodeError` and its owned issues for Go callers. A generated endpoint's
body decoder continues to provide its normal field paths in the public response;
table-specific rejections currently use the generic message.

Declaration/configuration faults, server source errors and output resource limits
remain internal failures; authorization, cancellation and capacity keep their
existing classifications. Never map every `fault.Invalid` to 400. No new DTO,
manifest version, database migration or catalog message is required. See the
[authenticated query consumer](../../tests/fixtures/consumer/reporting/http.go).

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

The SQL pattern operator `like` is discovered for text sources but disabled
until the declaration calls `AllowLike()`, because a client-chosen pattern can
defeat indexes. Call it before `Restrict` when the restricted set includes
`like`; on a `Related` filter, enable it on the child source.

Values are scalar text, including integers and decimals. Empty string remains a
string; SQL NULL uses `is_null`/`is_not_null`, without values. Boolean trees use
`and`, `or` and single-child `not`. AND may contain WHERE and HAVING filters;
mixed-phase OR/NOT fail because moving them between phases would change meaning.

Negation means "the condition is not true". `ne` and `not_in` on a nullable
column also match NULL rows, and `not` pushes its negation down to each
comparison, adding `IS NULL` for a nullable operand, so `not(note eq "memo")`
includes rows without a note. SQL `NOT` alone would silently drop them.
NULL tests negate exactly, and a double negation restores the original filter.

Global search ORs only declared searchable columns. It prefers `icontains` when
the source supports it, otherwise literal `contains`. PostgreSQL owns case
folding. `%`, `_` and `!` stay literal for these substring operators; `like`
permits SQL pattern syntax only when explicitly allowed. `Define` rejects a
table whose searchable columns span the WHERE and HAVING phases.

`table.Query(ctx, manager, authority, request)` returns `query.Page[R]` using one
count and one row query in a read-only repeatable-read transaction. The count
query omits the table's ordering. `table.SimpleQuery(...)` returns
`query.SimplePage[R]` from a single row query with one lookahead row and no
count; use it when a total is not needed. `table.Count(...)` applies the same
filters and authorization with one unordered count query. Pagination is
validated for all three; count represents the complete matching result. The
default `MaxOffset` of 10,000 rows (page 500 at 20 rows) bounds deep offset
scans; use filters or an export beyond it. Failed queries return no partial
page. Direct Go callers retain ownership of request slices until the operation
returns.

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

One awkward value never fails an export. Invalid UTF-8 becomes U+FFFD. A cell
longer than `MaxCellBytes`, or an XLSX cell longer than the 32,767 UTF-16 units
a spreadsheet cell holds, is cut at a character boundary and ends with
`…[truncated]`. CSV carries control characters literally; XLSX stores C0
controls and U+FFFE/U+FFFF with the OOXML `_xHHHH_` escape. The formatted
cells of one row are bounded by `MaxRowBytes` as a resource limit.

CSV uses standard delimiter/newline/quote escaping and prefixes potentially
interpreted formula text with an apostrophe: `=`, `+`, `-`, `@`, including after
leading Unicode whitespace, and leading tab/CR. This changes the literal CSV
value, including negative numeric text. Re-saving/re-importing CSV can change a
spreadsheet's interpretation; consumers must retain a safe import policy.
`ExportOptions.ByteOrderMark` prefixes CSV with a UTF-8 byte-order mark for
spreadsheet programs that otherwise guess a legacy encoding; XLSX rejects it.

XLSX uses one worksheet and a fixed style sheet. `ScalarCell`/`NullableCell`
columns become typed cells when exactly representable: integers and decimals
with at most 15 significant digits (integers use format `0`), finite floats,
booleans, dates (`yyyy-mm-dd`) and date-times (`yyyy-mm-dd hh:mm:ss`) from
1900-03-01 to 9999-12-31. Date-times are written as wall time in the export's
presentation time zone, since spreadsheets store no zone. Longer integers (such
as 16-digit identifiers) and decimals, other dates, enumerations and every `FormatWith` column stay
exact inline text. Formulas, macros, hyperlinks, external links and a growing
shared-string table are absent. XML escaping and OOXML literal-escape handling
preserve text and carriage returns. The ZIP has a fixed number of parts and
streams buffered worksheet rows. Both compressed file bytes and uncompressed XML
bytes are bounded; there is no workbook-sized memory buffer. These are resource
bounds, not a throughput guarantee.

Capacity has two levels. `MaxExports` bounds concurrent generation (query,
formatting and file writing); its slot is released as soon as the artifact is
complete, so a slow download or delivery never blocks other exports.
`MaxArtifacts` (at least `MaxExports`) bounds completed artifacts still open,
and so the temporary files on disk; each holds its slot until `Close`. When
either is exhausted, an export waits briefly and then fails with
`fault.Overloaded` (HTTP 503 with `Retry-After`). Close is serialized and
idempotent. Cancellation stops generation but does not abandon an executing
callback or open file; a completed artifact's reads end only at `Close` or
manager shutdown. Failed file removals remain manager-owned, prevent new exports,
and are retried by Close, the next export and manager shutdown. A failed export
never publishes a partial artifact. Retain the manager if shutdown reports a
cleanup failure so removal can be retried after the underlying filesystem issue
is repaired. Applications should use a private, application-owned temporary
directory and normal operational cleanup for process-crash leftovers.

Defaults include 32 query operations, 2 concurrent export generations, 16
retained artifacts, one-minute query and ten-minute export timeouts, page size
at most 1000, offset at most 10,000, 50,000 export rows, 64 MiB file bytes and
256 MiB XML. Individual encoded rows are limited to 256 KiB and their page sum
to 4 MiB; exported rows bound their formatted cells by the same row limit.
Cells are limited to 64 KiB (at least 64 bytes) and XLSX cells to 32,767 UTF-16
units, with truncation as described above. Requests have a 64 KiB encoded
limit, 128 filter nodes, bounded nesting, 100 values per filter, 4096 bytes per
scalar/search and 8 requested sort fields. `DefaultConfig` is the runtime source
of these defaults; `Validate` checks custom limits before any I/O.

## HTTP, jobs and lifecycle

`table.Download(...)` freezes a validated request and returns the existing typed
HTTP download. Opening it runs export authorization and scoping again. The
HTTP file response owns the artifact, ranges and cleanup. Its own file-response
limits still apply. Generation and transfer run under the request's single
deadline, so declare the download route with
`.WithTimeout(manager.DownloadTimeout())` instead of inheriting the kernel's
`RequestTimeout`. `Config.DownloadTimeout()` is twice `ExportTimeout` (20 minutes
by default, at most `http.MaxRouteTimeout`); generation is additionally bounded
by `ExportTimeout`. When the request ends through its deadline, a client
disconnect or shutdown, generation stops and promptly releases its export slot,
read transaction and temporary file. Deliver very large reports through a job.
Exports read committed rows only, in their own read-only snapshot. Each
response carries the artifact's SHA-256 as a strong `ETag` plus `Last-Modified`.
Every open regenerates the report, so a Range resume with `If-Range` receives
the complete new file when a later run differs instead of splicing two runs,
and `If-None-Match` returns 304 for identical content. See the
[consumer endpoint](../../tests/fixtures/consumer/reporting/http.go) and
[download guide](http-downloads.md).

`datatable.ExportHandler` adapts a table to an ordinary `jobs.Handler[P]`.
The resolver must reload current authority from a trusted queued reference; the
table then applies export authorization again. Delivery receives a completed
artifact, after its generation slot is released, and must finish reading before
returning. Its context retains the job's identity and the export deadline and
ends at manager shutdown; closing the same manager from delivery fails with a
cycle error. Cancellation cannot report success even if the delivery callback
returns nil. The handler closes the artifact on success, failure, panic and
Goexit. Jobs remain at-least-once; use the definition's
`CurrentID(ctx)` as an idempotent destination identity. The
[consumer declaration](../../tests/fixtures/consumer/reporting/export_job.go)
stores typed actor/request/presentation data, with no captured role claims.
Use the existing [jobs/outbox](jobs.md) path for durable transactional dispatch.

`datatable.Module` borrows database/locale dependencies and preserves their
lifetime until query and export-generation callbacks actually exit. Export
workers must depend on this module and shut down first. `Manager.Close(ctx)`
stops admission, cancels both operation groups and closes every completed
artifact that is still open: its reads report `fault.Closed` and its file is
removed. Completed artifacts use no borrowed dependency, so shutdown never
waits for their owners; the context bounds the caller's wait for callbacks.
Separate `DoneQueries`/`DoneExports` signals report actual exit. Self-shutdown
from an active table callback or export delivery fails before either group is
partially canceled.

`Registry.Descriptions()` contributes row/request schemas, labels, capability
allowlists and typed scalar metadata to the shared manifest. The [TypeScript emitter](client-contracts.md) consumes that shared manifest;
this package does not infer a second client schema.

## Importing CSV and XLSX

The `datatable/importer` package streams an uploaded table into a typed row
struct. Declare each heading once with its scalar codec and a typed assignment,
plus ordinary validation rules for the complete row. The
[consumer member import](../../tests/fixtures/consumer/reporting/member_import.go)
declares:

```go
importer.Define(importer.Spec[MemberImport]{
    Columns: []importer.Column[MemberImport]{
        importer.Field("Name", text, func(row *MemberImport, v string) { row.Name = v }),
        importer.NullableField("Nickname", text, func(row *MemberImport, v value.Nullable[string]) { row.Nickname = v }),
        importer.Field("State", foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor()), func(row *MemberImport, v State) { row.State = v }),
        importer.Field("Balance", foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), func(row *MemberImport, v decimal.Decimal) { row.Balance = v }),
    },
    Rules: []validation.Rule[MemberImport]{name.Rules(validation.NonBlank[string](), validation.MaxLength[string](80))},
})
```

Run it with a source and a handler that receives bounded chunks in file order:

```go
report, err := reporting.MemberImports.Run(ctx, importer.CSVSource(upload), func(ctx context.Context, rows []importer.Row[reporting.MemberImport]) error {
    return saveMembers(ctx, rows)
})
```

The heading row (the first row unless `HeadingRow` says otherwise) maps headings
after trimming spaces and ignoring case; a leading byte-order mark is ignored and
extra file columns are ignored. A missing `Field` heading or a repeated declared
heading returns a `*importer.HeadingError` (a `fault.Invalid`) with one issue per
heading before any row is read. A `NullableField` heading may be absent, and its
empty cells assign `value.Null`; an empty `Field` cell is a `required` issue.

Each non-blank row is parsed first; a cell its codec rejects is a `type` issue
at `/<heading>`, and an oversized cell a `length` issue. Rules run only on a
completely parsed row and report their own field paths. Invalid rows are skipped
and reported in `Report.Failures` with their row number: the worksheet row for
XLSX, or the line on which the record starts for CSV. `Report.Failed` counts all
of them; at most `MaxFailures` are retained. Valid rows reach the handler in
chunks of `ChunkSize`, and a handler error stops the import. Handled chunks are
not undone by a later failure: run the handler inside a caller-owned transaction
and check `Report.Failed` for an all-or-nothing import. Codec, rule and handler
callbacks run synchronously; panic and Goexit become faults.

`importer.CSVSource(reader)` accepts RFC 4180 quoting; `WithComma` selects
another delimiter. `importer.XLSXSource(readerAt, size)` reads the first
worksheet, or `WithSheet(name)`, streaming its XML: shared strings (including
rich-text runs, without phonetic guides), inline strings, formula results,
booleans and numbers. A floating-point column receives a number exactly as
stored. Every other column, including text, accepts only numbers a spreadsheet
shows without rounding (at most 15 significant digits); a stored number that
15 digits cannot represent, such as a 16-digit identifier or
`0.30000000000000004`, is a `type` issue instead of a silently rounded value.
Export such identifiers as text; datatable exports already do. In date,
date-time and local date-time columns a number is a serial date; date-times are
read as wall time in the spec's `TimeZone` (UTC by default). Error cells and
references to missing shared strings are `type` issues; a row containing only
such cells is reported as a failure, not skipped as blank. External
relationships are never opened.

`DefaultLimits` bounds 100,000 data rows, 1024 columns, 32 KiB cells, 1 MiB CSV
records, 64 MiB of input, 128 MiB of decompressed XML, one million shared
strings totalling 64 MiB, 1000 retained failures and 500-row chunks. The ZIP
central directory is checked (at most 4096 parts) before it is read, every
decompressed byte counts against the XML budget, and every XML token (text,
tag, attribute value, comment, CDATA section or processing instruction) is
bounded to eight times the cell limit plus 4 KiB whatever `<` or `>` it
contains, so compression bombs and oversized tokens fail with `fault.Invalid`
instead of exhausting memory. Document type declarations are rejected. Only the shared-string
table is held in memory; rows are never buffered beyond one chunk.
