import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
const sdk = await import(pathToFileURL(process.argv[2]).href);
const baseURL = process.argv[3];
const transport = async request => {
 const response = await fetch(request.url, { method: request.method, headers: request.headers, body: request.body, signal: request.signal });
 return { status: response.status, headers: Object.fromEntries(response.headers), body: new Uint8Array(await response.arrayBuffer()), close() {} };
};
const client = sdk.createClient(transport, { baseURL, headers: { Authorization: "Bearer alice" } });
const path = { team: "1", project: "shared" };
const patch = body => client.workflowPatch({ path, body });
let result = await patch({});
assert.equal(result.data.kind, "updated");
assert.equal(result.data.title, "Initial");
assert.equal(result.data.budget, "10");
result = await patch({ title: null });
assert.equal(result.data.title, null);
assert.equal(result.data.budget, "10");
result = await patch({ title: "", budget: "0" });
assert.equal(result.data.title, "");
assert.equal(result.data.budget, "0");
result = await patch({ title: "  From TypeScript  ", budget: "42" });
assert.equal(result.data.title, "From TypeScript");
assert.equal(result.data.budget, "42");
assert.deepEqual((await patch({})).data, result.data);
assert.deepEqual({ ...(await client.workflowShow({ path })).data }, { id: result.data.id, slug: result.data.slug, title: result.data.title, budget: result.data.budget });
await assert.rejects(() => client.workflowPatch({ path: { team: "1", project: "foreign" }, body: {} }), e => e instanceof sdk.APIError && e.status === 404);
await assert.rejects(() => client.workflowPatch({ path: { team: "2", project: "shared" }, body: {} }), e => e instanceof sdk.APIError && e.status === 403);
await assert.rejects(() => patch({ title: undefined }), sdk.ContractError);
await assert.rejects(() => patch({ budget: 0 }), sdk.ContractError);
const page = await client.workflowCatalogue({ query: { team: "1", page: "1", per_page: "1" } });
assert.deepEqual(page.data.map(item => ({ ...item })), [{ slug: "shared" }]);
assert.equal(page.meta.total, "1");
for (const name of ["Member", "null"]) {
 for (const [method, input] of [
  ["workflowRulesJson", { body: { name } }],
  ["workflowRulesForm", { body: { name } }],
  ["workflowRulesQuery", { query: { name } }],
  ["workflowRulesMultipart", { body: { name } }],
 ]) assert.deepEqual({ ...await client[method](input) }, { name });
}
// Server-side rule enforcement remains authoritative for raw callers too.
for (const [source, method, headers, body, suffix] of [
 ["json", "POST", {"Content-Type":"application/json"}, '{"name":"x"}', ""],
 ["form", "POST", {"Content-Type":"application/x-www-form-urlencoded"}, "name=x", ""],
 ["query", "GET", {}, undefined, "?name=x"],
]) {
 const response = await fetch(baseURL + "/rules/" + source + suffix, {method,headers,body});
 assert.equal(response.status,422);
 const error = await response.json();
 assert.ok(error.issues.some(issue => issue.path.endsWith("/name")));
}
const key = sdk.idempotencyKey("workflow-typescript-submission-0001");
const submission = { path, body: { name: "Generated client" }, idempotencyKey: key };
const accepted = await client.workflowSubmit(submission);
assert.equal(accepted.data.kind, "queued");
assert.equal(accepted.data.name, "Generated client");
assert.deepEqual(await client.workflowSubmit({ ...submission, body: {name:" Generated client "} }), accepted);
await assert.rejects(() => client.workflowSubmit({ ...submission, body: {name:"Changed"} }), e => e instanceof sdk.APIError && e.status === 409 && e.code === "idempotency_mismatch");
console.log("integrated PATCH, tagged envelopes, pagination, source rules and durable replay passed");
