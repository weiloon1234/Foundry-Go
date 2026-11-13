import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const sdk = await import(pathToFileURL(process.argv[2]));
const baseURL = process.argv[3];
const sent = [];
const transport = async request => {
  sent.push(request);
  assert.equal(request.headers["x-app-version"], "2.4.0 +/=%");
  assert.equal(request.headers["x-sequence"], "9223372036854775807");
  assert.equal(request.headers.authorization, "Bearer fixture-only");
  assert.equal(request.url, baseURL + "/headers?q=active+%2B%2F%3D%25");
  const response = await fetch(request.url, { headers: request.headers });
  return { status: response.status, headers: Object.fromEntries(response.headers), body: new Uint8Array(await response.arrayBuffer()), close: async () => {} };
};
const api = sdk.createClient(transport, { baseURL, headers: { Authorization: "Bearer fixture-only" } });
const headers = { "X-App-Version": "2.4.0 +/=%", "X-Sequence": "9223372036854775807" };
await api.headersInspect({ query: { q: "active +/=%" }, headers });
assert.deepEqual(headers, { "X-App-Version": "2.4.0 +/=%", "X-Sequence": "9223372036854775807" });
assert.equal(sent.length, 1);
const field = sdk.operation("headersInspect").field("headers", "X-App-Version");
assert.equal(field.path, "/headers/X-App-Version");
assert.equal(field.required, true);
assert.equal(sdk.operation("headersInspect").field("headers", "X-Sequence").required, false);

for (const request of [
  {}, { headers: null }, { headers: {} },
  { headers: { "x-app-version": "2.4.0" } },
  { headers: { ...headers, "x-app-version": "alias" } },
  { headers: { "X-App-Version": null } },
  { headers: { "X-App-Version": undefined } },
  { headers: { "X-App-Version": 2 } },
  { headers: { "X-App-Version": "2", "X-Sequence": 1 } },
  ...["\r", "\n", "\0", "\x7f", "é", "先", " leading", "trailing ", "\tleading", "trailing\t", "a".repeat(8193)].map(text => ({ headers: { "X-App-Version": text } })),
]) {
  await assert.rejects(api.headersInspect(request), error => error instanceof sdk.ContractError && error.issues.some(issue => issue.path.startsWith("/headers")));
}
for (const raw of [{ "x-app-version": "override" }, { "X-Sequence": "1" }, { "x-SeQuEnCe": "1" }]) {
  await assert.rejects(api.headersInspect({ headers: { "X-App-Version": "2" } }, { headers: raw }), sdk.ContractError);
  await assert.rejects(sdk.createClient(transport, { headers: raw }).headersInspect({ headers: { "X-App-Version": "2" } }), sdk.ContractError);
}
assert.equal(sent.length, 1, "invalid headers reached transport");

const captures = [];
const capture = sdk.createClient(async request => {
  captures.push(request);
  return { status: 204, headers: {}, body: "", close: async () => {} };
});
await capture.headersInspect({ headers: { "X-App-Version": "" } });
assert.equal(captures[0].headers["x-app-version"], "");
assert.equal(Object.hasOwn(captures[0].headers, "x-sequence"), false);
await capture.headersInspect({ headers: { "X-App-Version": "a\tb", "X-Sequence": "0" } });
assert.equal(captures[1].headers["x-app-version"], "a\tb");
assert.equal(captures[1].headers["x-sequence"], "0");
assert.equal(sdk.validateRequest("headersInspect", { headers: { "X-App-Version": "2" } }).complete, true);
