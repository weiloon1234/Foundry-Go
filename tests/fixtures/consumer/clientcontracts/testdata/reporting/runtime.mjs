import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
const sdk = await import(pathToFileURL(process.argv[2]).href);
const baseURL = process.argv[3];
const metadata = sdk.contractMetadata();
assert.deepEqual(metadata.tables.map(table => table.id), ["reports.members", "reports.numeric_levels", "reports.orders", "reports.totals"]);
const members = metadata.tables.find(table => table.id === "reports.members");
const orders = metadata.tables.find(table => table.id === "reports.orders");
const totals = metadata.tables.find(table => table.id === "reports.totals");
assert.equal(members.columns.find(column => column.name === "nickname").filter.nullable, true);
assert.deepEqual(members.columns.find(column => column.name === "state").filter.operators, ["eq", "in", "ne", "not_in"]);
assert.equal(orders.columns.find(column => column.name === "memberName").searchable, true);
assert.equal(totals.columns.find(column => column.name === "total").filter.phase, "having");
const id = "0193fd8c-2075-7000-8000-000000000001";
for (const [type, value] of [
  ["MemberRow", { id, name: "Ada", nickname: null, state: "active", balance: "9007199254740993.125" }],
  ["NumericLevelRow", { id, level: 0, wideLevel: "9007199254740993" }],
  ["OrderRow", { id, memberName: "Ada", label: "Priority", amount: "9007199254740993.125" }],
  ["MemberTotal", { memberId: id, count: "9223372036854775807", total: "9007199254740993.125" }],
  ["MemberTotal", { memberId: id, count: "0", total: null }],
]) {
  const key = `foundry.test/consumer/reporting.${type}`;
  const encoded = sdk.encodeContract(key, sdk.contractValue(key, value));
  assert.deepEqual({ ...sdk.decodeReceived(key, encoded) }, value);
}
const transport = async request => {
  const response = await fetch(request.url, { method: request.method, headers: request.headers, body: request.body, signal: request.signal });
  return { status: response.status, headers: Object.fromEntries(response.headers), body: response.body, close: async () => { if (response.body && !response.body.locked) await response.body.cancel(); } };
};
const api = sdk.createClient(transport, { baseURL });
await assert.rejects(api.reportsMembersQuery({ body: {} }), error => error instanceof sdk.APIError && error.status === 401);
await assert.rejects(api.reportsMembersCsv({}), error => error instanceof sdk.APIError && error.status === 401);

// S2: typed datatable descriptor and query/export helpers
assert.equal(sdk.tables["reports.members"].id, "reports.members");
assert.equal(typeof sdk.tableQueries["reports.members"], "function");
assert.equal(typeof sdk.tableDownloads["reports.members"], "function");
assert.equal(sdk.tables["reports.members"].columns.find(column => column.name === "state").kind, "enum");
assert.deepEqual(sdk.tables["reports.members"].columns.find(column => column.name === "state").enum.cases, ["active", "disabled"]);
for (const [name, enumeration, cases] of [
  ["level", sdk.AgentLevel, [0, 1]],
  ["wideLevel", sdk.WideLevel, ["9007199254740993", "9007199254740994"]],
]) {
  const column = sdk.tables["reports.numeric_levels"].columns.find(column => column.name === name);
  assert.equal(column.enum, enumeration);
  assert.deepEqual(column.enum.cases, cases);
  assert.equal(Object.hasOwn(column, "cases"), false);
}
assert.equal(sdk.AgentLevel.labels[0], "reports.agent_level.member_level");
assert.equal(sdk.WideLevel.labels["9007199254740993"], "reports.wide_level.wide_member_level");
await assert.rejects(sdk.tableQueries["reports.members"](api, { body: {} }), error => error instanceof sdk.APIError && error.status === 401);
await assert.rejects(sdk.tableDownloads["reports.members"](api, {}), error => error instanceof sdk.APIError && error.status === 401);

console.log("PASS reporting tables, exact/nullable DTO codecs and authenticated HTTP contracts");
