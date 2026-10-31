# Explicit datatable operation bindings — Part A

2026-10-31. Implemented in `lane/table-bindings`; targeted acceptance passed.
No full `make verify`, race, app migration, release tag, or Git write is claimed.

## Delivered behavior

- `contract.TableID` is the sole table identifier definition, with no datatable alias.
- HTTP endpoint bindings retain table/kind/request-field metadata through configuration,
  scopes, authentication, signing, model binding, idempotency, and owned snapshots.
  Factories cannot add, drop, or substitute a binding while retaining transport types.
- Manifest 7 carries `Operation.TableOperation` and `Table.Page`. Build and Decode
  share normalization, canonical page validation, and global query/export/download
  slot uniqueness. Ticket and file exports compete for the export slot.
- Projection retains tables only through selected bindings. Shared request or row
  schemas and historical route IDs provide no association. The full manifest still
  retains metadata-only tables.
- Metadata lives in `tables`. Calls live only in `tableQueries`, `tableExports`, and
  `tableDownloads`, each one mapped type over its own keys. `tableExportKinds`
  retains literal ticket/file kinds. Received row maps and the canonical page family
  preserve unknown enum values, non-null rows, links, and lossless counts.
- Ticket requests/responses remain application-owned. File operations return
  `FileResult`; the caller retains its close obligation. Query helpers require a
  plain canonical numbered page, so status-tagged response alternatives are rejected.

The only design revision is the lead-authorized split of metadata and operation
registries after the original mapped-type intersection failed generic correlation.
There are no open design questions. Part B remains outside this change.

## Public Go API

```go
// contract
// Kinds: TableQuery, TableTicketExport, TableFileExport, TableDownload.
type TableID string
type TableOperationKind string
type TableOperationBinding struct {
    Table        TableID
    Kind         TableOperationKind
    RequestField string
}

// http
func TableOperation(contract.TableOperationBinding) EndpointOption

// datatable
func QueryEndpoint[S, R, A, P, Q any](
    Table[S, R, A], http.Route[P], http.Query[Q], ...http.EndpointOption,
) http.Endpoint[P, Q, Request, pagination.NumberedResponse[R]]

func BindTicketExport[S, R, A, P, Q, B, T any](
    Table[S, R, A], http.Endpoint[P, Q, B, T], validation.Field[B, Request],
) http.Endpoint[P, Q, B, T]

func DownloadEndpoint[S, R, A, P, Q any](
    Table[S, R, A], http.Route[P], http.Query[Q],
    http.Response[http.Download], ...http.EndpointOption,
) http.Endpoint[P, Q, http.NoBody, http.Download]

func BindFileExport[S, R, A, P, Q, B any](
    Table[S, R, A], http.Endpoint[P, Q, B, http.Download],
) http.Endpoint[P, Q, B, http.Download]
```

`Description.Page` derives from `pagination.NumberedJSON`. The shared internal
schema-template composition validates decoded pages without a Go row value.
The new identifier fixture emits the schema identity
`github.com/weiloon1234/Foundry-Go/contract.TableID` and TypeScript `TableID = string`;
the wire value remains a string. New exports have GoDoc, and the public documentation
backlog and pinned method lists were not expanded or rewritten.

## Changed files by owner

| Owner | Files |
| --- | --- |
| Contract/manifest | `contract/table_operation.go`; manifest document, build, normalization, projection, and regression sources |
| HTTP | table option, endpoint storage/snapshots, contribution equality, pagination schema composition, and adapter/factory regression sources |
| Datatable | endpoint constructors/binders; identifier migration; canonical page description and normalization; metadata ownership tests |
| Internal schema | `internal/contractmeta/page.go` |
| TypeScript | table renderer, reserved names, runtime metadata type, binding renderer tests |
| Consumers | reporting routes; ticket/file/download declarations and generated DTOs; strict generic adapter/runtime fixtures; six compiler negatives in each fixture |
| Release bookkeeping | Go-way case lists, sorted mapping, summary/count, classifier regression; generated current-contract cohorts and ownership receipts |
| Documentation | datatable/client/HTTP/workflow guides, compatibility, changelog, master status, this report |

The public-config implementation regions, all three apps, frozen contract cohorts,
and frozen oracles are unchanged. New `.ts`/`.mjs` fixtures already fit the release
source allowlist. Only `testdata/current-contracts` was republished.

## Commands and results

All Go commands used the native toolchain and warm cache with `GOFLAGS=-trimpath`.
The initial sandbox denial was resolved by authorized cache access; no cache was cleared.

| Command | Result |
| --- | --- |
| `go build ./...` | Pass on final implementation |
| `go vet ./http ./datatable ./contract ./contract/manifest ./http/pagination ./typescript ./internal/contractmeta ./internal/generate` | Pass |
| `cd tests/fixtures/consumer && go vet ./clientcontracts ./reporting` | Pass |
| Targeted framework/generator tests below | Pass in all seven packages |
| Additional projection/presentation/module/publication tests below | Pass |
| `go test ./datatable -run '^TestSerializedTableMetadataKeepsRowFiltersAndOrderingConsistent$'` | Pass |
| `go test . -run '^TestConsumerAPI(MethodVisibility|Documentation|DSLGoDocSurface)$'` | Pass |
| New compile-negative cases below, in consumer and Go-way | All six pass in each fixture |
| `make generate` | Pass in resumed segments: framework completed, consumer dependency metadata load initially failed; after the build, consumer generated five descriptors and Go-way generated no changes |
| `make generate-check` | Pass in all 15 generation roots |
| `make publish-client-contracts` | Pass; current cohort only |
| `cd tests/fixtures/consumer && go test -count=1 ./clientcontracts` | Pass; native TypeScript tools were exercised separately in the required target |
| `make typescript-check` with the environment below | Pass, including strict generic tables, runtime calls, real HTTP/WebSocket clients, and React/Vue adapters |
| `make release-tools-check` | Pass in segments: release Go vet/tests and other Python modules passed; the catalogue module was corrected and rerun successfully |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'goway_catalogue_test.py'` | Pass, 12 tests; fixes rule text after gofmt and recognizes the real datatable QueryEndpoint import |
| `make docs-check` | Pass |
| `git diff --check` | Pass |

Framework and generator selection:

```sh
go test -timeout=20m ./http ./datatable ./contract ./contract/manifest ./http/pagination ./typescript ./internal/generate -run 'Test(Table|Manifest|Schema|Pagination|Page|Render|RouteFactoriesCannotSubstituteDeclaredTransport|SignedAuthenticatedModelBindingPreservesAllBoundaries|NamedGenericCustomContractsShareGeneratedAndRuntimeIdentity|DTOValidationPathNamingIsStable|JSONPathNamesSeparateSegments|DTOJSONLocalsDoNotShadowPayloadTypes)' -count=1
go test ./contract/manifest ./typescript -run '^Test(Projection.*|Presentation.*|ExtractFileConstraints|ClientGenerationPublishesAllAdaptersTogether|SurfacesPublishPortalEntriesBesideSharedRuntimeModules|ConcurrentRenderingReturnsOwnedModules)$'
```

Compiler selection, run separately from `tests/fixtures/consumer` and
`tests/fixtures/goway`:

```sh
go test -count=1 . -run '^TestInvalidPublicContractsFailCompilation/datatable_(query_handler_row|query_handler_request|query_guard_actor|ticket_field_owner|ticket_field_value|file_export_response)$'
```

Native TypeScript environment:

```sh
FOUNDRY_TEST_NODE=$(command -v node)
FOUNDRY_TEST_TYPESCRIPT=/Users/weiloon/Projects/rust-go/Foundry-Go/tools/typescript/node_modules/typescript/lib/tsc.js
```

The first strict run reproduced the original design's generic-correlation failure.
The revised registries pass both literal and generic calls. The fixture also checks
exact query output equivalence, received enums, wide counts, required path/query
inputs, absent keys, and rejection of an incompatible optional export view.
A source audit found no assertions, `any`, computed client calls, or operation-name
lookup in emitted table helpers or the app adapter.

## Emitted TypeScript

Generated from the actual
[consumer declarations](../../tests/fixtures/consumer/clientcontracts/table_bindings.go).
`reports.members` has query, ticket-export, and download bindings. The maps below
also show the other fixture tables that prove missing slots and direct files.

```typescript
export interface TableRows {
  readonly "reports.members": MemberRow;
  readonly "reports.numeric_levels": NumericLevelRow;
  readonly "reports.orders": OrderRow;
  readonly "reports.totals": MemberTotal;
}

export interface TableReceivedRows {
  readonly "reports.members": MemberRowReceived;
  readonly "reports.numeric_levels": NumericLevelRowReceived;
  readonly "reports.orders": OrderRow;
  readonly "reports.totals": MemberTotal;
}
export type TableId = keyof TableRows;
export type TableRow<Id extends TableId> = TableRows[Id];
export type TableReceivedRow<Id extends TableId> = TableReceivedRows[Id];
export type TablePageReceived<Row> = { readonly "data": ReadonlyArray<Row>; readonly "links": Links; readonly "meta": NumberedMeta };
export interface TableQueryInputs {
  readonly "reports.members": Operations["adminInventory"]["request"];
  readonly "reports.numeric_levels": Operations["adminLevels"]["request"];
}
export interface TableQueryOperationNames {
  readonly "reports.members": "adminInventory";
  readonly "reports.numeric_levels": "adminLevels";
}
export type QueryTableId = keyof TableQueryInputs;
export interface TableExportInputs {
  readonly "reports.members": Operations["issueReport"]["request"];
  readonly "reports.orders": Operations["reportsDirect"]["request"];
}
export interface TableExportOutputs {
  readonly "reports.members": Operations["issueReport"]["response"];
  readonly "reports.orders": FileResult;
}
export interface TableExportOperationNames {
  readonly "reports.members": "issueReport";
  readonly "reports.orders": "reportsDirect";
}
export interface TableExportKinds {
  readonly "reports.members": "ticket";
  readonly "reports.orders": "file";
}
export type ExportTableId = keyof TableExportInputs;
export interface TableDownloadInputs {
  readonly "reports.members": Operations["filesOpen"]["request"];
  readonly "reports.totals": Operations["filesAggregate"]["request"];
}
export interface TableDownloadOutputs {
  readonly "reports.members": FileResult;
  readonly "reports.totals": FileResult;
}
export interface TableDownloadOperationNames {
  readonly "reports.members": "filesOpen";
  readonly "reports.totals": "filesAggregate";
}
export type DownloadTableId = keyof TableDownloadInputs;
export type TableQueryOutputs = { readonly [Id in QueryTableId]: TablePageReceived<TableReceivedRows[Id]> };
export type Tables = { readonly [Id in TableId]: TableMetadata<TableRows[Id]> & { readonly id: Id } };
export type TableQueries = { readonly [Id in QueryTableId]: (client: API, input: TableQueryInputs[Id], options?: CallOptions) => Promise<TableQueryOutputs[Id]> };
export type TableExports = { readonly [Id in ExportTableId]: (client: API, input: TableExportInputs[Id], options?: CallOptions) => Promise<TableExportOutputs[Id]> };
export type TableDownloads = { readonly [Id in DownloadTableId]: (client: API, input: TableDownloadInputs[Id], options?: CallOptions) => Promise<TableDownloadOutputs[Id]> };
```

The `reports.members` metadata entry in `export const tables: Tables` is:

```typescript
  "reports.members": Object.freeze<Tables["reports.members"]>({
    id: "reports.members",
    columns: [
      {
        name: "id",
        label: "reports.member.id",
        kind: "text",
        nullable: false,
        sortable: true,
        searchable: false,
        exportable: true,
        operators: [],
      },
      {
        name: "name",
        label: "reports.member.name",
        kind: "text",
        nullable: false,
        sortable: true,
        searchable: true,
        exportable: true,
        operators: ["between", "contains", "eq", "gt", "gte", "icontains", "in", "lt", "lte", "ne", "not_in"],
      },
      {
        name: "nickname",
        label: "reports.member.nickname",
        kind: "text",
        nullable: true,
        sortable: true,
        searchable: false,
        exportable: true,
        operators: ["between", "contains", "eq", "gt", "gte", "icontains", "in", "is_not_null", "is_null", "lt", "lte", "ne", "not_in"],
      },
      {
        name: "state",
        label: "reports.member.state",
        kind: "enum",
        nullable: false,
        sortable: false,
        searchable: false,
        exportable: true,
        operators: ["eq", "in", "ne", "not_in"],
        enum: State,
      },
      {
        name: "balance",
        label: "reports.member.balance",
        kind: "decimal",
        nullable: false,
        sortable: true,
        searchable: false,
        exportable: true,
        operators: ["between", "eq", "gt", "gte", "in", "lt", "lte", "ne", "not_in"],
      },
    ],
    defaultSort: [
      { column: "name", direction: "asc" },
    ],
    exports: true,
  }),
```

The emitted operation registries use concrete client properties:

```typescript
export const tableQueries: TableQueries = /* @__PURE__ */ Object.freeze<TableQueries>({
  "reports.members"(client: API, input: Operations["adminInventory"]["request"], options?: CallOptions) {
    return client.adminInventory(input, options);
  },
  "reports.numeric_levels"(client: API, input: Operations["adminLevels"]["request"], options?: CallOptions) {
    return client.adminLevels(input, options);
  },
});
export const tableExports: TableExports = /* @__PURE__ */ Object.freeze<TableExports>({
  "reports.members"(client: API, input: Operations["issueReport"]["request"], options?: CallOptions) {
    return client.issueReport(input, options);
  },
  "reports.orders"(client: API, input: Operations["reportsDirect"]["request"], options?: CallOptions) {
    return client.reportsDirect(input, options);
  },
});
export const tableDownloads: TableDownloads = /* @__PURE__ */ Object.freeze<TableDownloads>({
  "reports.members"(client: API, input: Operations["filesOpen"]["request"], options?: CallOptions) {
    return client.filesOpen(input, options);
  },
  "reports.totals"(client: API, input: Operations["filesAggregate"]["request"], options?: CallOptions) {
    return client.filesAggregate(input, options);
  },
});
export const tableExportKinds: TableExportKinds = /* @__PURE__ */ Object.freeze<TableExportKinds>({
  "reports.members": "ticket",
  "reports.orders": "file",
});
```

## Final generic adapter fixture

This is the adapter portion of the
[strict fixture](../../tests/fixtures/consumer/clientcontracts/testdata/table-bindings/types.ts).
The rest of that file contains literal, generic, absent-key, and output-equivalence checks.

```typescript
import * as sdk from "./contracts_foundry.gen.js";

type Request = sdk.ContractTypes["github.com/weiloon1234/Foundry-Go/datatable.Request"];
type TableExportRequest = sdk.ContractTypes["foundry.test/consumer/clientcontracts.TableExportRequest"];
type TableExportResource = sdk.ContractTypes["foundry.test/consumer/clientcontracts.TableExportResource"];
type ExportFormat = TableExportRequest["format"];
type TablePage<Row> = sdk.TablePageReceived<Row>;
interface TableMemory { readonly page: number }
interface StoreApi<T> { getState(): T }
interface TableSource<DeclaredRow, ReceivedRow = DeclaredRow> {
  readonly metadata: sdk.TableMetadata<DeclaredRow>;
  query(request: Request, signal?: AbortSignal): Promise<TablePage<ReceivedRow>>;
  readonly export: ((format: ExportFormat, request: Request) => Promise<TableExportResource>) | undefined;
  readonly memory: StoreApi<TableMemory> | undefined;
}
type AdminExportTransports = {
  readonly [Id in sdk.QueryTableId]?: (client: sdk.API, input: { readonly body: TableExportRequest }, options?: sdk.CallOptions) => Promise<TableExportResource>;
};
const exportTransports: AdminExportTransports = sdk.tableExports;
declare const api: sdk.API;
declare const tableMemory: StoreApi<TableMemory>;
export function adminTable<Id extends sdk.QueryTableId>(id: Id): TableSource<sdk.TableRow<Id>, sdk.TableReceivedRow<Id>> {
  const descriptor = sdk.tables[id];
  const exportCall = exportTransports[id];
  return {
    metadata: descriptor,
    query: (request, signal) => sdk.tableQueries[id](api, { body: request }, signal ? { signal } : {}),
    export: exportCall ? (format, request) => exportCall(api, { body: { format, request } }) : undefined,
    memory: tableMemory,
  };
}
```

## Source evidence

The current-contract cohort retains its generator ownership manifests and publication
receipt. The aggregate SHA-256 below covers the sorted changed Go, TypeScript, MJS,
and Python paths (excluding the current cohort), using `path + NUL + bytes + NUL`:

`62b4cec7e50e2176ff14705e13bfdbd23e5953f23c3b08356f2644c41d01c6fc`
