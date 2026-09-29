import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const sdk = await import(pathToFileURL(process.argv[2]).href);
const baseURL = process.argv[3];
const type = "foundry.test/consumer/clientcontracts.Payload";
const order = "0193fd8c-2075-7000-8000-000000000001";
const buyer = "0193fd8c-2075-7000-8000-000000000002";
const source = {
  id: order, buyer_id: buyer, natural: "MY", large: "9223372036854775807", counter: "18446744073709551615",
  amount: "999999999999999999999999999999.123456789", state: "ready", nullable: null,
  quoted: "-9223372036854775808", exact: "1.234567890123456789e400", when: "2026-09-17T12:34:56.123456789Z",
  day: "2024-02-29", clock: "23:59:59.123456789", local: "2026-09-17T12:34:56.123456789", duration: "P1M2DT0.000003S",
  bytes: "AQID", keys: { "9223372036854775807": "max", "-9223372036854775808": "min" }, tags: ["one", "two"],
};
const payload = sdk.contractValue(type, source);
const encoded = sdk.encodeContract(type, payload);
assert.ok(encoded.includes('"large":9223372036854775807'));
assert.ok(encoded.includes('"counter":18446744073709551615'));
assert.ok(encoded.includes('"quoted":"-9223372036854775808"'));
assert.ok(encoded.includes('"exact":1.234567890123456789e400'));
assert.equal(sdk.decodeContract(type, encoded).large, source.large);
assert.equal(Object.getPrototypeOf(payload), null);
assert.equal(Object.hasOwn(payload, "optional"), false);
assert.equal(sdk.contractValue(type, { ...source, optional: null }).optional, null);
for (const change of [
  { large: 9223372036854775807 }, { large: "9223372036854775808" }, { large: "1e2" }, { counter: "-1" },
  { counter: "18446744073709551616" }, { state: "unknown" }, { nullable: undefined }, { optional: undefined },
  { amount: "1e10" }, { when: "2026-02-30T12:00:00Z" }, { when: "2026-09-17T24:00:00Z" },
  { bytes: "AB==" }, { id: "wrong-uuid" }, { keys: { "01": "not canonical" } }, { tags: ["ok", null] }, { unknown: true },
  { day: "2025-02-29" }, { clock: "24:00:00" }, { local: "2026-09-17T12:00:00Z" }, { duration: "P999999999999Y" }, { duration: "PT0.0000001S" },
]) assert.throws(() => sdk.contractValue(type, { ...source, ...change }), sdk.ContractError);
const missing = { ...source }; delete missing.nullable;
assert.throws(() => sdk.contractValue(type, missing), sdk.ContractError);
// Server output decodes tolerantly: additive properties and enum cases do not
// break a deployed client, while strict decoding and requests still reject them.
const additive = encoded.replace('"state":"ready"', '"state":"archived","added":{"nested":[1]}');
const tolerated = sdk.decodeReceived(type, additive);
assert.ok(tolerated.state instanceof sdk.UnknownEnumValue); assert.equal(tolerated.state.value, "archived");
assert.equal(Object.hasOwn(tolerated, "added"), false); assert.equal(tolerated.large, source.large);
assert.throws(() => sdk.decodeContract(type, additive), sdk.ContractError);
assert.throws(() => sdk.encodeContract(type, tolerated), sdk.ContractError);
assert.throws(() => sdk.decodeReceived(type, encoded.replace('"large":9223372036854775807', '"large":"text"')), sdk.ContractError);
for (const text of [encoded + "false", encoded.replace('"natural":"MY"', '"natural":"MY","natural":"US"'), encoded.replace('"natural":"MY"', '"natural":"\\ud800"'), encoded.replace('"large":9223372036854775807', '"large":9.223372036854776e18')]) {
  assert.throws(() => sdk.decodeContract(type, text), sdk.ContractError);
}
assert.throws(() => new sdk.JSONNumber("NaN"), sdk.ContractError);
const dynamic = sdk.decodeContract("encoding/json.RawMessage", '{"big":18446744073709551615,"exact":1e400,"__proto__":{"safe":true}}');
assert.equal(dynamic.big.text, source.counter); assert.equal(dynamic.exact.text, "1e400"); assert.equal(Object.getPrototypeOf(dynamic), null);
assert.ok(sdk.encodeContract("encoding/json.RawMessage", dynamic).includes('"exact":1e400'));
assert.equal({}.safe, undefined);
const tiny = { Bytes: 32, Depth: 2, Nodes: 8, Steps: 16, Issues: 1 };
assert.throws(() => sdk.decodeContract(type, encoded, tiny), sdk.ContractError);
assert.throws(() => sdk.encodeContract(type, payload, tiny), sdk.ContractError);
for (let i = 0; i < 64; i++) {
  const natural = `text-${i}-\"\\\n\t-é-🧱`;
  assert.equal(sdk.decodeContract(type, sdk.encodeContract(type, { ...payload, natural })).natural, natural);
}
// Object names consume nodes, matching the Go parser's exact wire budget.
for (const [id, input, nodes] of [
 ["encoding/json.RawMessage", {}, 1],
 ["encoding/json.RawMessage", { key: "value" }, 3],
 ["encoding/json.RawMessage", { outer: { key: "value" } }, 5],
 ["foundry.test/consumer/unions.PaymentMethod", { kind: "bank_transfer", reference: "value" }, 5],
]) {
 const limits = { Bytes: 4096, Depth: 16, Nodes: nodes, Steps: 256, Issues: 8 };
 const wire = sdk.encodeContract(id, input, limits);
 assert.deepEqual(sdk.decodeContract(id, wire, limits), sdk.contractValue(id, input));
 if (nodes > 1) {
  const exceeded = { ...limits, Nodes: nodes - 1 };
  assert.throws(() => sdk.encodeContract(id, input, exceeded), e => e instanceof sdk.ContractError && e.issues.some(issue => issue.code === "limit"));
  assert.throws(() => sdk.decodeContract(id, wire, exceeded), e => e instanceof sdk.ContractError && e.issues.some(issue => issue.code === "limit"));
 }
}
// Explicit schemas may omit key constraints while retaining typed map values.
const labelsType = "foundry.test/consumer/clientcontracts.ExplicitLabels";
const labels = sdk.decodeContract(labelsType, '{"":"empty","01":"text","__proto__":"ordinary","é/~/":"unicode"}');
assert.equal(Object.getPrototypeOf(labels), null);
assert.equal(labels.__proto__, "ordinary");
assert.deepEqual(sdk.decodeContract(labelsType, sdk.encodeContract(labelsType, labels)), labels);
assert.equal(sdk.decodeContract(labelsType, "null"), null);
assert.equal(sdk.encodeContract(labelsType, null), "null");
assert.equal(Object.keys(sdk.decodeContract(labelsType, "{}")).length, 0);
for (const input of ['{"key":1}', '{"key":null}', '{"key":"one","key":"two"}']) {
  assert.throws(() => sdk.decodeContract(labelsType, input), sdk.ContractError);
}
for (const input of [{ key: 1 }, { key: undefined }, { key: null }]) {
  assert.throws(() => sdk.encodeContract(labelsType, input), sdk.ContractError);
}
assert.throws(() => sdk.encodeContract(labelsType, labels, { ...tiny, Steps: 1 }), sdk.ContractError);
const metadata = sdk.contractMetadata();
assert.equal(metadata.version.text, String(sdk.manifestVersion));
assert.ok(metadata.locales.supported.includes("ms"));
assert.equal(metadata.permissions[0].label_key, "permissions.manage");
assert.equal(metadata.tables[0].id, "reports.members");
assert.equal(metadata.notifications[0].channels[0].payload, "foundry.test/consumer/clientcontracts.Member");
assert.ok(metadata.enums[0].cases.some(item => item.label_key === "enum.localization.status.ready"));
assert.equal(sdk.manifestJSON.includes("must-not-be-exported"), false);

let calls = 0, closes = 0;
const transport = async request => {
  calls++;
  assert.ok(request.body === undefined || typeof request.body === "string" || request.body instanceof Blob || request.body instanceof ReadableStream);
  if (request.body instanceof Blob) assert.ok(request.body.size <= request.maxBodyBytes);
  assert.ok(request.redirect === "follow" || request.redirect === "manual");
  const result = await fetch(request.url, { method: request.method, headers: request.headers, body: request.body, signal: request.signal, credentials: request.credentials, redirect: request.redirect, ...(request.duplex ? { duplex: request.duplex } : {}) });
  return { status: result.status, headers: Object.fromEntries(result.headers), body: result.body ?? new Uint8Array(), close: async () => { closes++; if (result.body && !result.body.locked) await result.body.cancel(); } };
};
const api = sdk.createClient(transport, { baseURL });
const formRequest = { query: { name: "query" }, body: { name: "  Jane  ", "tags[]": ["first", "second"] } };
const formCheck = sdk.validateRequest("formsSubmit", formRequest);
assert.equal(formCheck.complete, false);
assert.deepEqual(formCheck.skipped, ["request_preparation"]);
const formResult = await api.formsSubmit(formRequest);
assert.equal(formResult.name, "Jane"); assert.equal(formResult.title, "Member"); assert.equal(formResult.query, "query");
assert.deepEqual([...formResult.tags], ["first", "second"]);
await assert.rejects(api.formsSubmit({ body: { name: "x" } }), error => error instanceof sdk.APIError && error.status === 422);
await assert.rejects(api.formsSubmit({ body: { name: "denied" } }), error => error instanceof sdk.APIError && error.status === 403);
await assert.rejects(api.formsSubmit({ body: { name: "Jane", blocked: "supplied" } }), error => error instanceof sdk.APIError && error.status === 422);
for (const body of [null, { name: "Jane", "tags[]": Array(17).fill("x") }, { name: "x".repeat(600) }, { name: "Jane", title: null }, { name: "Jane", "name[nested]": "x" }]) {
  await assert.rejects(api.formsSubmit({ body }), sdk.ContractError);
}
const formMeta = metadata.http.find(operation => operation.name === "formsSubmit");
assert.equal(formMeta.body.media_type, "application/x-www-form-urlencoded"); assert.equal(formMeta.preparation, true);
const genericResult = await api.genericEcho({ body: { id: order, name: "Generic user", sequence: "9223372036854775807", note: null } });
assert.equal(genericResult.data.name, "Generic user");
assert.equal(genericResult.data.sequence, "9223372036854775807");
assert.equal(genericResult.data.note, null);
assert.equal(genericResult.trace, "fixture");
const omittedGeneric = await api.genericEcho({ body: { id: order, name: "No note", sequence: "0" } });
assert.equal(Object.hasOwn(omittedGeneric.data, "note"), false);
const genericProject = await api.genericProject({});
assert.equal(genericProject.data.title, "Typed project");
const paymentType = "foundry.test/consumer/unions.PaymentMethod";
const cardPayment = { kind: "card", token: "fixture-token", sequence: "9223372036854775807", labels: ["one"] };
const wrappedPayment = { kind: "wrapped", data: { token: "fixture-token", sequence: "7" }, trace: "wrapped" };
for (const method of [cardPayment, { kind: "bank_transfer", reference: "fixture-bank" }, wrappedPayment]) {
  const response = await api.unionsEcho({ body: { method, more: [method], maybe: null } });
  assert.deepEqual(JSON.parse(JSON.stringify(response.data.method)), method);
  assert.equal(response.data.more[0].kind, method.kind);
  assert.equal(response.data.maybe, null);
}
const omittedUnion = await api.unionsEcho({ body: { method: cardPayment } });
assert.equal(Object.hasOwn(omittedUnion.data, "maybe"), false);
assert.ok(sdk.encodeContract(paymentType, cardPayment).includes('"sequence":9223372036854775807'));
for (const method of [{}, {kind: 7}, {kind: "future"}, {...cardPayment, reference: "mixed"}, {...cardPayment, token: null}, {...cardPayment, sequence: 1}]) {
  assert.throws(() => sdk.contractValue(paymentType, method), sdk.ContractError);
}
for (const wire of ['{"kind":"card","kind":"bank_transfer","reference":"duplicate"}', '{"kind":"future"}', '{"kind":null}']) {
  assert.throws(() => sdk.decodeContract(paymentType, wire), sdk.ContractError);
}
assert.throws(() => sdk.contractValue("foundry.test/consumer/unions.PaymentRequest", {method:{kind:7}}), error => error instanceof sdk.ContractError && error.issues[0].path === "/method/kind");
const beforeEchoClose = closes;
const request = { path: { key: source.large }, query: { q: "spaces + / Unicode é", tag: ["first", "second"] }, body: payload };
const response = await api.itemsEcho(request);
assert.equal(response.large, source.large); assert.equal(response.counter, source.counter); assert.equal(response.exact, source.exact);
assert.equal(response.quoted, source.quoted); assert.equal(response.amount, source.amount); assert.equal(response.natural, "MY");
assert.equal(response.keys[source.large], "max"); assert.equal(response.when, source.when);
assert.equal(closes, beforeEchoClose + 1);
for (const duration of ["P1Y2M3W4DT5H6M7.000008S", "@ 1 year 2 mons 3 days 4 hours 5 mins 6.000007 secs", "1-2 3 04:05:06.000007", "-1 day -02:03:04", "0"]) {
  const response = await api.itemsEcho({ ...request, body: { ...payload, duration } }); assert.equal(typeof response.duration, "string");
}
const validation = sdk.validateRequest("itemsEcho", { ...request, body: { ...payload, natural: " \u0085 " } });
assert.equal(validation.issues[0].path, "/body/natural");
assert.equal(validation.issues[0].code, "foundry.non_blank");
assert.equal(validation.issues[0].message, "Country must not be blank.");
const localized = sdk.validateRequest("itemsEcho", { ...request, body: { ...payload, natural: " " } }, { validationMessages: { locale: "ms", translations: { "fields.country": "Negara", "validation.non_blank": "{{attribute}} diperlukan." } } });
assert.equal(localized.issues[0].message, "Negara diperlukan.");
assert.equal(localized.issues[0].label, "Negara");
assert.equal(localized.complete, false);
assert.ok(localized.skipped.includes("foundry.uppercase"));
const invalidTranslation = sdk.validateRequest("itemsEcho", { ...request, body: { ...payload, natural: " " } }, { validationMessages: { locale: "ms", translations: { "validation.non_blank": "{{private_input}}" } } });
assert.equal(invalidTranslation.issues[0].message, "Country must not be blank.");
// Repeated placeholders must stay within the shared message output budget,
// including when a translated label is much longer than the field's wire name.
const boundedMessage = sdk.validateRequest("itemsEcho", { ...request, body: { ...payload, natural: " " } }, { validationMessages: { locale: "ms", translations: { "fields.country": "X".repeat(4096), "validation.non_blank": "{{attribute}}".repeat(5000) } } });
assert.equal(boundedMessage.issues[0].message, "X".repeat(4096) + " must not be blank.");
const oversizedTemplate = sdk.validateRequest("itemsEcho", { ...request, body: { ...payload, natural: " " } }, { validationMessages: { locale: "ms", translations: { "validation.non_blank": "X".repeat(65537) } } });
assert.equal(oversizedTemplate.issues[0].message, "Country must not be blank.");
const tooLong = { ...request, body: { ...payload, natural: "X".repeat(65) } };
const pluralOptions = { validationMessages: { locale: "ar", translations: { "validation.max_length": { one: "{{attribute}} واحد {{max}}", other: "{{attribute}} أحرف {{max}}" } } } };
assert.equal(sdk.validateRequest("itemsEcho", tooLong, pluralOptions).issues[0].message, "Country أحرف 64");
assert.equal(sdk.validateRequest("itemsEcho", tooLong).issues[0].message, "Country must contain at most 64 characters.");
const fallbackOptions = { validationMessages: { locale: "ms", translations: {}, fallback: pluralOptions.validationMessages } };
assert.equal(sdk.validateRequest("itemsEcho", tooLong, fallbackOptions).issues[0].message, "Country أحرف 64");
const decimalPluralOptions = { validationMessages: { locale: "ru", translations: {
  "cart.items": { many: "Rounded to zero.", other: "Fractional count." },
} } };
assert.equal(sdk.validateRequest("pluralBoundary", { body: "" }, decimalPluralOptions).issues[0].message, "Fractional count.");
for (const operation of ["pluralTiny", "pluralNegative"]) {
  assert.equal(sdk.validateRequest(operation, { body: "" }, decimalPluralOptions).issues[0].message, "Enter a value.");
}
const tinyPlural = await fetch(baseURL + "/plural/tiny", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify("") });
assert.equal(tinyPlural.status, 422);
assert.equal((await tinyPlural.json()).issues[0].message, "Enter a value.");
assert.equal(validation.complete, false);
assert.ok(validation.skipped.includes("foundry.uppercase"));
const serverRule = await fetch(baseURL + "/items/" + source.large, { method: "POST", headers: { "content-type": "application/json" }, body: encoded.replace('"natural":"MY"', '"natural":"my"') });
assert.equal(serverRule.status, 422);
assert.equal((await serverRule.json()).issues[0].code, "foundry.uppercase");

const beforeValidation = calls;
await assert.rejects(api.itemsEcho({ ...request, body: { ...payload, natural: " " } }), sdk.ContractError);
assert.equal(calls, beforeValidation);
const bad = await fetch(baseURL + "/items/" + source.large, { method: "POST", headers: { "content-type": "application/json" }, body: encoded.replace('"natural":"MY"', '"natural":" "') });
assert.equal(bad.status, 422); const nativeIssues = await bad.json(); assert.equal(nativeIssues.issues[0].path, "/body/natural");
await assert.rejects(api.failure({}), error => error instanceof sdk.APIError && error.status === 409 && error.code === "consumer_changed" && error.response.status === "409");
await assert.rejects(api.accountShow({}), error => error instanceof sdk.APIError && error.status === 401);
const authenticated = sdk.createClient(transport, { baseURL, headers: { cookie: "fixture_session=fixture-only" } });
assert.equal((await authenticated.accountShow({})).display, "Visible member");
for (const name of ["membersSecure", "membersSecureSimple", "membersSecureCursor"]) {
  await assert.rejects(api[name]({}), error => error instanceof sdk.APIError && error.status === 401);
  const securedPage = await authenticated[name]({});
  assert.equal(securedPage.meta.per_page, "20");
  assert.ok(!JSON.stringify(securedPage).includes("must-not-be-exported"));
  if (name !== "membersSecureCursor") assert.equal(securedPage.data[0].email, "Visible member");
}
await assert.rejects(authenticated.membersSecure({ query: { per_page: "101" } }));
const page = await api.membersIndex({});
assert.equal(page.meta.current_page, "1"); assert.equal(page.meta.per_page, "20"); assert.equal(page.meta.total, "0");
assert.equal(page.links.next, null); assert.equal(page.links.prev, null); assert.deepEqual(page.data, []);
await assert.rejects(api.membersIndex({ query: { page: "0" } }));
assert.equal(await api.empty({}), undefined);
const uploaded = await api.uploadsProfile({ body: { document: { filename: "fixture.txt", data: new Blob(["upload body"], { type: "text/plain" }) }, settings: { caption: "JSON part", note: null }, "tags[]": ["one", "two"] } });
assert.equal(uploaded.bytes, "11");
const download = await api.download({}); let file = "";
try { for await (const bytes of download.body) file += new TextDecoder().decode(bytes); } finally { await download.close(); }
assert.equal(file, "client download");
const partial = await api.download({}, { headers: { range: "bytes=0-5" } });
assert.equal(partial.status, 206); let prefix = "";
for await (const bytes of partial.body) prefix += new TextDecoder().decode(bytes);
assert.equal(prefix, "client");
const ranges = await api.download({}, { headers: { range: "bytes=0-0,3-3,6-6" } });
assert.equal(ranges.status, 206); let rangeBytes = 0;
for await (const bytes of ranges.body) rangeBytes += bytes.byteLength;
assert.ok(rangeBytes > 15);
// Several declared success statuses: the client returns the received one.
const created = await api.membersUpsert({ body: { display: "new" } });
assert.equal(created.status, 201); assert.equal(created.body.display, "new"); assert.ok(Object.isFrozen(created));
const updated = await api.membersUpsert({ body: { display: "existing" } });
assert.equal(updated.status, 200); assert.equal(updated.body.display, "existing");
assert.deepEqual(metadata.http.find(operation => operation.name === "membersUpsert").statuses.map(status => status.text), ["200", "201"]);
await assert.rejects(sdk.createClient(async () => ({ status: 202, headers: { "content-type": "application/json" }, body: '{"display":"x"}', close: () => {} })).membersUpsert({ body: { display: "x" } }), sdk.ContractError);
// Redirects are returned, not followed, and only relative targets are accepted.
let redirectMode;
const continued = await sdk.createClient(async request => { redirectMode = request.redirect; return transport(request); }, { baseURL }).sessionContinue({});
assert.equal(redirectMode, "manual"); assert.equal(continued.status, 303); assert.equal(continued.location, "/account");
for (const result of [{ status: 0, headers: {} }, { status: 303, headers: { location: "//evil.test/" } }, { status: 303, headers: { location: "https://evil.test/" } }, { status: 303, headers: {} }, { status: 302, headers: { location: "/account" } }]) {
  await assert.rejects(sdk.createClient(async () => ({ body: "", close: () => {}, ...result })).sessionContinue({}), sdk.ContractError);
}
// Typed server-sent events decode each event's data and resume after Last-Event-ID.
const eventStream = await api.membersEvents({});
assert.equal(eventStream.status, 200);
const streamed = [];
for await (const event of eventStream) streamed.push(event);
assert.deepEqual(streamed.map(event => [event.id, event.name, event.data.display]), [["1", "member", "member 1"], ["2", "member", "member 2"], ["3", "member", "member 3"]]);
assert.equal(streamed[2].retry, 1500); assert.ok(Object.isFrozen(streamed[0]));
await assert.rejects(async () => { for await (const ignored of eventStream) void ignored; }, sdk.ContractError);
const resumed = [];
for await (const event of await api.membersEvents({}, { headers: { "last-event-id": "2" } })) resumed.push(event.id);
assert.deepEqual(resumed, ["3"]);
const unread = await api.membersEvents({}); await unread.close();
const framed = sdk.createClient(async () => ({ status: 200, headers: { "content-type": "text/event-stream" }, body: (async function* () {
  yield new TextEncoder().encode(": comment\r\nid: 7\r\nevent: member\r\ndata: {\"display\":\r");
  yield new TextEncoder().encode("\ndata: \"split\",\"added\":1}\r\n\r\ndata: {\"display\":\"unterminated\"}");
})(), close: () => {} }));
const parsed = [];
for await (const event of await framed.membersEvents({})) parsed.push(event);
assert.equal(parsed.length, 1); assert.equal(parsed[0].id, "7"); assert.equal(parsed[0].data.display, "split"); assert.equal(Object.hasOwn(parsed[0].data, "added"), false);
const oversized = sdk.createClient(async () => ({ status: 200, headers: { "content-type": "text/event-stream" }, body: "data: " + "x".repeat(40 << 20) + "\n\n", close: () => {} }));
await assert.rejects(async () => { for await (const ignored of await oversized.membersEvents({})) void ignored; }, sdk.ContractError);
// Signed links: absolute, origin-relative (sent to the base URL), permanent and
// decorated with a declared ignored parameter all reach the signed route.
const links = JSON.parse(process.argv[5]);
for (const signedURL of [links.absolute, links.relative, links.permanent, links.decorated]) {
  assert.equal((await api.membersSigned({ signedURL })).display, "signed member");
}
const signedMeta = metadata.http.find(operation => operation.name === "membersSigned").route.signed_url;
assert.equal(signedMeta.permanent, true); assert.equal(signedMeta.relative, true); assert.deepEqual([...signedMeta.ignored_parameters], ["utm_source"]);
for (const signedURL of [links.absolute + "&extra=1", links.absolute.replace(/&signature=/, "&expires=1&signature="), "https://evil.test" + links.relative, links.relative.replace("/members/signed", "/members/other")]) {
  assert.throws(() => sdk.validateRequest("membersSigned", { signedURL }), sdk.ContractError);
}
const tampered = links.absolute.replace(/signature=[^&]+/, "signature=v1.fixture.AAAA");
await assert.rejects(api.membersSigned({ signedURL: tampered }), error => error instanceof sdk.APIError && error.status === 403);
// Raw bodies: Blob, bytes and streams of declared media types, bounded client side.
const rawText = await api.filesRaw({ body: { data: new Blob(["raw text"]), mediaType: "text/plain" } });
assert.equal(rawText.bytes, "8"); assert.equal(rawText.detected_type, "text/plain");
assert.equal((await api.filesRaw({ body: { data: new Uint8Array([1, 2, 3]), mediaType: "application/octet-stream" } })).bytes, "3");
const chunks = new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(1000)); controller.enqueue(new Uint8Array(24)); controller.close(); } });
assert.equal((await api.filesRaw({ body: { data: chunks, mediaType: "application/octet-stream" } })).bytes, "1024");
await assert.rejects(api.filesRaw({ body: { data: new Blob(["x"]), mediaType: "image/png" } }), sdk.ContractError);
await assert.rejects(api.filesRaw({ body: { data: new Uint8Array(65 << 10), mediaType: "application/octet-stream" } }), sdk.ContractError);
await assert.rejects(api.filesRaw({ body: { data: new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(65 << 10)); controller.close(); } }), mediaType: "application/octet-stream" } }));
assert.equal((await fetch(baseURL + "/raw", { method: "POST", headers: { "content-type": "application/octet-stream" }, body: new Uint8Array(65 << 10) })).status, 413);
assert.equal((await fetch(baseURL + "/raw", { method: "POST", headers: { "content-type": "image/png" }, body: new Uint8Array(1) })).status, 415);
const unconsumed = await api.download({}); const beforeClose = closes; await unconsumed.close(); await unconsumed.close(); assert.equal(closes, beforeClose + 1);
await assert.rejects(async () => { for await (const ignored of unconsumed.body) void ignored; }, sdk.ContractError);
const controller = new AbortController(); controller.abort(); const beforeAbort = calls;
await assert.rejects(api.itemsEcho(request, { signal: controller.signal })); assert.equal(calls, beforeAbort);

for (const result of [
  { status: 200, body: encoded }, // The actual endpoint declares 201.
  { status: 201, body: '{"wrong":"shape"}' },
  { status: 201, body: encoded, headers: { "content-type": "text/plain" } },
  { status: 409, body: '{"status":500,"error_code":"conflict","message":"Invalid"}' },
  { status: 201, body: new Uint8Array(5 << 20) },
]) {
  let closed = 0;
  const badClient = sdk.createClient(async () => ({ headers: { "content-type": "application/json" }, ...result, close: () => { closed++; } }));
  await assert.rejects(badClient.itemsEcho(request), sdk.ContractError); assert.equal(closed, 1);
}
const additiveTransport = async () => ({ status: 201, headers: { "content-type": "application/json" }, body: additive, close: () => {} });
const additiveResult = await sdk.createClient(additiveTransport).itemsEcho(request);
assert.ok(additiveResult.state instanceof sdk.UnknownEnumValue); assert.equal(Object.hasOwn(additiveResult, "added"), false);
await assert.rejects(sdk.createClient(additiveTransport, { strictResponses: true }).itemsEcho(request), sdk.ContractError);
const brokenCleanup = sdk.createClient(async () => ({ status: 201, headers: { "content-type": "application/json" }, body: "{}", close: () => { throw new Error("cleanup failure"); } }));
await assert.rejects(brokenCleanup.itemsEcho(request), sdk.ContractError);
const readFailure = new Error("stream read failed");
const brokenFile = sdk.createClient(async () => ({ status: 200, headers: { "content-type": "text/plain" }, body: (async function* () { yield new Uint8Array([1]); throw readFailure; })(), close: () => { throw new Error("cleanup failure"); } }));
const failedDownload = await brokenFile.download({});
await assert.rejects(async () => { for await (const chunk of failedDownload.body) void chunk; }, error => error === readFailure);

async function socket(path = "/ws") {
  const native = new WebSocket(baseURL.replace(/^http/, "ws") + path, sdk.realtimeSubprotocol);
  await new Promise((resolve, reject) => { native.addEventListener("open", resolve, { once: true }); native.addEventListener("error", () => reject(new Error("native socket failed")), { once: true }); });
  return { protocol: native.protocol, send: text => native.send(text), close: () => native.close(), listen(receive, closed) {
    const message = event => receive(event.data), close = () => closed();
    native.addEventListener("message", message); native.addEventListener("close", close); native.addEventListener("error", close);
    return () => { native.removeEventListener("message", message); native.removeEventListener("close", close); native.removeEventListener("error", close); };
  } };
}
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; }
async function withDeadline(promise) { let timer; try { return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("fixture deadline")), 5000); })]); } finally { clearTimeout(timer); } }
const errors = [], realtime = sdk.createRealtime(await socket(), { onError: error => errors.push(error) });
const room = realtime.channels.updates("7"), event = deferred();
room.on.updated((value, info) => event.resolve({ value, info }));
assert.deepEqual(await room.subscribe(), []);
let accepted = 0; await room.publish.relay(payload, { onAccepted: () => { accepted++; } });
const publication = await withDeadline(event.promise);
assert.equal(accepted, 1); assert.equal(publication.value.counter, source.counter); assert.equal(publication.info.replayed, false);
await room.unsubscribe(); room.dispose(); realtime.close(); assert.deepEqual(errors, []);
const reconnect = sdk.createRealtime(await socket()); const replayRoom = reconnect.channels.updates("7"), replay = deferred();
replayRoom.on.updated((value, info) => replay.resolve({ value, info })); await replayRoom.subscribe({ replay: 1 });
const historical = await withDeadline(replay.promise); assert.equal(historical.info.replayed, true); assert.equal(historical.value.large, source.large);
await replayRoom.unsubscribe(); replayRoom.dispose(); reconnect.close();
const anonymous = sdk.createRealtime(await socket());
await assert.rejects(anonymous.channels.accounts("7").subscribe(), error => error instanceof sdk.RealtimeError && error.code === "unauthenticated"); anonymous.close();
const privateClient = sdk.createRealtime(await socket("/ws-auth"));
await assert.rejects(privateClient.channels.accounts("8").subscribe(), error => error instanceof sdk.RealtimeError && error.code === "forbidden");
const account = privateClient.channels.accounts("7"); const members = await account.subscribe(); assert.equal(members.length, 1); assert.equal(members[0].data.display, "Visible member"); assert.equal(members[0].connections, 1);
await account.unsubscribe(); account.dispose(); privateClient.close();

function fakeSocket() {
  let receive, closed, didClose = 0; const sent = [];
  return { sent, get didClose() { return didClose; }, receive: frame => receive(typeof frame === "string" ? frame : JSON.stringify(frame)),
    transport: { protocol: sdk.realtimeSubprotocol, send: text => sent.push(JSON.parse(text)), close: () => { didClose++; }, listen: (r, c) => { receive = r; closed = c; return () => {}; } },
    disconnect: () => closed(),
  };
}
const fake = fakeSocket(), client = sdk.createRealtime(fake.transport), selected = client.channels.updates("7");
const joining = selected.subscribe(); const join = fake.sent.at(-1);
fake.receive({ v: 1, type: "subscribed", id: join.id, channel: join.channel, room: join.room }); await joining;
let delivered = 0, received, receivedInfo;
selected.on.updated((value, info) => { delivered++; received = value; receivedInfo = info; });
const frame = { v: 1, type: "event", channel: "updates", room: "7", event: "updated", message_id: order };
const eventJSON = JSON.stringify(frame).replace(/}$/, ',"payload":' + encoded + "}"); fake.receive(eventJSON); fake.receive(eventJSON); assert.equal(delivered, 1);
assert.ok(Object.isFrozen(received)); assert.ok(Object.isFrozen(received.tags)); assert.ok(Object.isFrozen(receivedInfo));
let interim = 0, completed = false; const publishing = selected.publish.relay(payload, { onAccepted: () => interim++ }).then(() => { completed = true; });
const pending = fake.sent.at(-1); fake.receive({ v: 1, type: "accepted", id: pending.id, channel: pending.channel, room: pending.room });
await Promise.resolve(); assert.equal(interim, 1); assert.equal(completed, false);
fake.receive({ v: 1, type: "ack", id: pending.id, channel: pending.channel, room: pending.room }); await publishing;
client.close(); assert.equal(fake.didClose, 1);

// Void callback contracts also accept async functions. Their failures must be
// observed without unhandled rejections or changing the acknowledgement state.
const asyncSocket = fakeSocket(), callbackErrors = [];
const asyncClient = sdk.createRealtime(asyncSocket.transport, { onError: async error => { callbackErrors.push(error.code); throw new Error("observer failure"); } });
const asyncRoom = asyncClient.channels.updates("7"), asyncJoining = asyncRoom.subscribe();
asyncSocket.receive({ v: 1, type: "subscribed", id: "r1", channel: "updates", room: "7" }); await asyncJoining;
asyncRoom.on.updated(async () => { throw new Error("listener failure"); });
asyncSocket.receive(eventJSON);
await new Promise(resolve => setTimeout(resolve, 0));
assert.deepEqual(callbackErrors, ["callback_failed"]); assert.equal(asyncSocket.didClose, 0);
const asyncPublish = asyncRoom.publish.relay(payload, { onAccepted: async () => { throw new Error("acceptance observer failure"); } });
const asyncMessage = asyncSocket.sent.at(-1);
asyncSocket.receive({ v: 1, type: "accepted", id: asyncMessage.id, channel: "updates", room: "7" });
await new Promise(resolve => setTimeout(resolve, 0));
assert.deepEqual(callbackErrors, ["callback_failed", "callback_failed"]);
asyncSocket.receive({ v: 1, type: "ack", id: asyncMessage.id, channel: "updates", room: "7" }); await asyncPublish;
asyncClient.close(); assert.equal(asyncSocket.didClose, 1);

const changingSocket = fakeSocket(), changingClient = sdk.createRealtime(changingSocket.transport), changingRoom = changingClient.channels.updates("7");
const changingJoin = changingRoom.subscribe();
changingSocket.receive({ v: 1, type: "subscribed", id: "r1", channel: "updates", room: "7" }); await changingJoin;
let initialCalls = 0, addedCalls = 0;
const removeInitial = changingRoom.on.updated(() => { initialCalls++; removeInitial(); changingRoom.on.updated(() => addedCalls++); });
changingSocket.receive(eventJSON); assert.equal(initialCalls, 1); assert.equal(addedCalls, 0);
changingSocket.receive(eventJSON.replace('"message_id":"' + order + '"', '"message_id":"' + buyer + '"'));
assert.equal(initialCalls, 1); assert.equal(addedCalls, 1);
changingRoom.on.updated(() => changingClient.close());
let afterClose = 0; changingRoom.on.updated(() => afterClose++);
changingSocket.receive(eventJSON.replace('"message_id":"' + order + '"', '"message_id":"0193fd8c-2075-7000-8000-000000000003"'));
assert.equal(afterClose, 0); assert.equal(changingSocket.didClose, 1);

for (const badFrame of ["{", '{"v":1,"v":1}', { v: 2, type: "subscribed", id: "r1", channel: "updates", room: "7" }, { v: 1, type: "ack", id: "wrong", channel: "updates", room: "7" }, { v: 1, type: "subscribed", id: "r1", channel: "updates", room: "different" }]) {
  const fake = fakeSocket(), errors = [], client = sdk.createRealtime(fake.transport, { onError: error => errors.push(error) });
  const joining = client.channels.updates("7").subscribe(); const failed = assert.rejects(joining, sdk.RealtimeError); fake.receive(badFrame); await failed;
  assert.equal(fake.didClose, 1); assert.equal(errors.length, 1);
}
for (const corruption of ["payload", "direction"]) {
  const fake = fakeSocket(), errors = [], client = sdk.createRealtime(fake.transport, { onError: error => errors.push(error) }), room = client.channels.updates("7");
  const ready = room.subscribe(); fake.receive({ v: 1, type: "subscribed", id: "r1", channel: "updates", room: "7" }); await ready;
  const badEvent = corruption === "payload" ? eventJSON.replace('"counter":18446744073709551615', '"counter":-1') : eventJSON.replace('"event":"updated"', '"event":"relay"');
  fake.receive(badEvent); assert.equal(fake.didClose, 1); assert.equal(errors.length, 1);
}
const canceled = fakeSocket(), cancelClient = sdk.createRealtime(canceled.transport), abort = new AbortController();
const waiting = cancelClient.channels.updates("7").subscribe({ signal: abort.signal }); const aborted = assert.rejects(waiting, error => error.code === "aborted"); abort.abort(); await aborted; assert.equal(canceled.didClose, 1);
assert.throws(() => sdk.createRealtime({ ...fakeSocket().transport, protocol: "foundry.v99" }), sdk.ContractError);
if (process.argv[4]) {
  const legacy = await import(pathToFileURL(process.argv[4]).href);
  assert.equal(legacy.manifestVersion, sdk.manifestVersion);
  assert.throws(() => legacy.decodeContract(paymentType, sdk.encodeContract(paymentType, wrappedPayment)), legacy.ContractError);
  const oldUnionClient = legacy.createClient(transport, { baseURL });
  const oldUnionResponse = await oldUnionClient.unionsEcho({ body: { method: cardPayment } });
  assert.equal(oldUnionResponse.data.method.token, cardPayment.token);
  const oldClient = legacy.createClient(transport, { baseURL }); assert.equal(oldClient.empty, undefined);
  assert.equal((await oldClient.itemsEcho(request)).large, source.large);
}
console.log("PASS exact codecs, strict payloads, validation, HTTP status/errors/files/pagination/alternative statuses/redirects/raw bodies/typed event streams/signed link forms, realtime auth/replay/presence/acks/cancellation, additive client compatibility");
