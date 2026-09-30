import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
const sdk = await import(pathToFileURL(process.argv[2]));
const echo = sdk.operation("itemsEcho");
const limit = echo.metadata.limits.Body.Bytes;
assert.ok(limit instanceof sdk.JSONNumber);
assert.equal(limit.text, "9223372036854775807");
assert.deepEqual(sdk.schema("client.ExactEnum").choices, ["9223372036854775807"]);
assert.deepEqual(sdk.schema("client.QuotedExactEnum").choices, ["9223372036854775807"]);
assert.equal(sdk.schema("client.NullableEnum").nullable, true);
assert.equal(sdk.schema("client.RequiredEnum").nullable, false);
assert.throws(() => { limit.text = "1"; }, TypeError);
assert.equal(echo.field("body", "large").schema.bits, 64);
// Every exported body source is inspected through the same location API.
const metadata = sdk.contractMetadata();
for (const op of metadata.http) {
  if (op.body?.media_type === "multipart/form-data") {
    const limits = sdk.operation(op.name).metadata.limits;
    for (const key of ["Readers", "Issues"]) assert.equal(limits.Multipart[key], Number(op.limits.Multipart[key].text));
    for (const part of op.body.parts) {
      const descriptor = sdk.operation(op.name).field("body", part.name);
      assert.equal(descriptor.parameter.kind, part.kind);
      assert.equal(descriptor.required, part.required);
      assert.equal(descriptor.repeated, part.repeated);
      if (part.kind === "file") {
        assert.equal(descriptor.schema, undefined);
        assert.throws(() => descriptor.field("filename"), sdk.ContractError);
      }
    }
  }
  if (op.preparation) {
    const descriptor = sdk.operation(op.name);
    assert.equal(descriptor.preparation, true);
    assert.deepEqual(descriptor.validation, sdk.operation(op.name).metadata.validation);
  }
}
assert.equal(sdk.operation("accountShow").metadata.route.authentication.credential.name, "fixture_session");
// An owned generic snapshot cannot mutate shared descriptors or client state.
metadata.http.find(op => op.name === "itemsEcho").route.path = "/changed";
assert.equal(sdk.operation("itemsEcho").metadata.route.path, "/items/{key}");
