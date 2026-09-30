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
// Descriptors and the metadata they expose are frozen; choices decode once per type.
const exactChoices = sdk.schema("client.ExactEnum").choices;
assert.throws(() => { exactChoices.push("1"); }, TypeError);
assert.equal(sdk.schema("client.ExactEnum").choices, exactChoices);
assert.throws(() => { echo.metadata.limits.Body.Bytes = 1; }, TypeError);
assert.throws(() => { echo.field("body", "amount").presentation.kind = "text"; }, TypeError);
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
    // Compare with the independent generic snapshot, not the descriptor's own cache.
    const plain = value => JSON.parse(JSON.stringify(value, (_, item) => item instanceof sdk.JSONNumber ? Number(item.text) : item));
    assert.deepEqual(plain(descriptor.validation), plain(op.validation));
  }
}
assert.equal(sdk.operation("accountShow").metadata.route.authentication.credential.name, "fixture_session");
// Bound collection entries use the codec's own key rules and declared lengths.
const index = sdk.schema("foundry.test/consumer/clientcontracts.OwnerIndex"), user = "0190a8f0-0000-7000-8000-000000000001";
assert.equal(index.field("owners").at(user).path, `/owners/${user}`);
assert.equal(index.field("owners").element().schema.kind, "string");
for (const key of [user.toUpperCase(), "00000000-0000-0000-0000-000000000000", "not-a-uuid"]) assert.throws(() => index.field("owners").at(key), sdk.ContractError);
assert.throws(() => index.field("owners").field(user), sdk.ContractError);
assert.equal(index.field("states").field("draft").path, "/states/draft");
assert.equal(index.field("states").at("ready").path, "/states/ready");
assert.throws(() => index.field("states").at("archived"), sdk.ContractError);
assert.equal(index.field("pair").at(1).path, "/pair/1");
assert.throws(() => index.field("pair").at(2), sdk.ContractError);
for (const key of ["abc", "01", "9223372036854775808"]) assert.throws(() => echo.field("body", "keys").at(key), sdk.ContractError);
// An owned generic snapshot cannot mutate shared descriptors or client state.
metadata.http.find(op => op.name === "itemsEcho").route.path = "/changed";
assert.equal(sdk.operation("itemsEcho").metadata.route.path, "/items/{key}");
