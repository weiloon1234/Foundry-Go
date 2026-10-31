import assert from "node:assert/strict";
import {pathToFileURL} from "node:url";
const sdk = await import(pathToFileURL(process.argv[2]));
const calls = [];
const ticket = {url: "/file?ticket=one", expires_at: "123"};
const page = {data: [], meta: {current_page: "1", per_page: "20", total: "0", last_page: "0"}, links: {next: null, prev: null}};
let closed = 0;
const file = {close: async () => {closed++;}};
const operation = (name, result) => async (input, options) => {calls.push({name,input,options});return result;};
const api = {
 adminInventory: operation("query",page), adminLevels: operation("levels",page),
 issueReport: operation("ticket",ticket), filesOpen: operation("download",file),
 reportsDirect: operation("direct",file), filesAggregate: operation("aggregate",file),
};
const options = {signal: new AbortController().signal};
assert.equal(await sdk.tableQueries["reports.members"](api,{body:{}},options),page);
assert.equal(calls[0].options,options);
assert.equal(await sdk.tableExports["reports.members"](api,{body:{format:"csv",request:{}}}),ticket);
assert.equal(await sdk.tableDownloads["reports.members"](api,{query:{ticket:"one",format:"csv"}}),file);
await file.close();
assert.equal(closed,1);
assert.equal(await sdk.tableExports["reports.orders"](api,{path:{key:"1"},query:{ticket:"one",format:"csv"}}),file);
await sdk.tableQueries["reports.numeric_levels"](api,{body:{}});
await sdk.tableDownloads["reports.totals"](api,{});
assert.deepEqual(calls.map(call=>call.name),["query","ticket","download","direct","levels","aggregate"]);
assert.equal("reports.numeric_levels" in sdk.tableExports,false);
assert.equal("reports.orders" in sdk.tableQueries,false);
assert.equal(sdk.tableExportKinds["reports.members"],"ticket");
assert.equal(sdk.tableExportKinds["reports.orders"],"file");
assert.equal(Object.isFrozen(sdk.tables),true);
assert.equal(Object.isFrozen(sdk.tables["reports.members"]),true);

assert.equal("query" in sdk.tables["reports.members"], false);
assert.equal("export" in sdk.tables["reports.members"], false);
assert.equal("download" in sdk.tables["reports.members"], false);
for (const registry of [sdk.tableQueries, sdk.tableExports, sdk.tableDownloads, sdk.tableExportKinds]) assert.equal(Object.isFrozen(registry), true);
