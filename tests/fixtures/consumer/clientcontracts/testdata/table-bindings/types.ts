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
export function query<Id extends sdk.QueryTableId>(id: Id, input: sdk.TableQueryInputs[Id]): Promise<sdk.TableQueryOutputs[Id]> {
  return sdk.tableQueries[id](api, input);
}
export function exportTable<Id extends sdk.ExportTableId>(id: Id, input: sdk.TableExportInputs[Id]): Promise<sdk.TableExportOutputs[Id]> {
  return sdk.tableExports[id](api, input);
}
export function download<Id extends sdk.DownloadTableId>(id: Id, input: sdk.TableDownloadInputs[Id]): Promise<sdk.TableDownloadOutputs[Id]> {
  return sdk.tableDownloads[id](api, input);
}
export async function literalChecks(): Promise<void> {
  const ticket: TableExportResource = await sdk.tableExports["reports.members"](api, { body: { format: "csv", request: {} } });
  const file: sdk.FileResult = await sdk.tableDownloads["reports.members"](api, { query: { ticket: ticket.url, format: "csv" } });
  await file.close();
  const kind: "ticket" = sdk.tableExportKinds["reports.members"];
  const fileKind: "file" = sdk.tableExportKinds["reports.orders"];
  void sdk.tableExports["reports.orders"](api, { path: { key: "1" }, query: { ticket: "a", format: "csv" } });
  // @ts-expect-error A query-only table has no export method.
  void sdk.tableExports["reports.numeric_levels"](api, {});
  // @ts-expect-error A direct-export-only table has no query method.
  void sdk.tableQueries["reports.orders"](api, { body: {} });
  // @ts-expect-error A download-only table has no query method.
  void sdk.tableQueries["reports.totals"](api, { body: {} });
  // @ts-expect-error Path and query inputs remain required.
  void sdk.tableExports["reports.orders"](api, {});
  // @ts-expect-error Tickets use the complete request wrapper.
  void sdk.tableExports["reports.members"](api, { body: {} });
  // @ts-expect-error Formats are the declared enum.
  void sdk.tableExports["reports.members"](api, { body: { format: "pdf", request: {} } });
  const page = await sdk.tableQueries["reports.members"](api, { body: {} });
  const total: string = page.meta.total;
  const rows: ReadonlyArray<sdk.TableReceivedRow<"reports.members">> = page.data;
  // @ts-expect-error Rows preserve the table's concrete type.
  const otherRows: ReadonlyArray<sdk.TableReceivedRow<"reports.numeric_levels">> = page.data;
  void [kind, fileKind, total, rows, otherRows];
}

declare const memberPage: sdk.TableQueryOutputs["reports.members"];
declare const operationPage: sdk.Operations["adminInventory"]["response"];
const pageToOperation: sdk.Operations["adminInventory"]["response"] = memberPage;
const operationToPage: sdk.TableQueryOutputs["reports.members"] = operationPage;
declare const receivedMember: sdk.TableReceivedRow<"reports.members">;
// @ts-expect-error Received enums admit unknown values; metadata enums stay closed.
const closedState: sdk.TableRow<"reports.members">["state"] = receivedMember.state;
// @ts-expect-error Metadata has no second operation invocation path.
void sdk.tables["reports.members"].query;
// @ts-expect-error A query-only table has no download key.
void sdk.tableDownloads["reports.numeric_levels"];
// @ts-expect-error The checked optional view rejects an incompatible ticket protocol.
const incompatibleExport: { readonly [Id in sdk.QueryTableId]?: (client: sdk.API, input: { readonly body: TableExportRequest }) => Promise<sdk.FileResult> } = sdk.tableExports;
void [pageToOperation, operationToPage, closedState, incompatibleExport];
