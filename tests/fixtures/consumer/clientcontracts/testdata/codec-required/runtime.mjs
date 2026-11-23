import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const sdk = await import(pathToFileURL(process.argv[2]));
const baseURL = process.argv[3];

const createOp = sdk.operation("codecCreate");
const patchOp = sdk.operation("codecPatch");
const quoteOp = sdk.operation("codecQuote");

const createState = sdk.formFieldState();
const bindCreate = sdk.fieldBinder((key) => key, () => createState);
const validID = "0193fd8c-2075-7000-8000-000000000001";
const createValues = {
  owner_id: "",
  omitzero_id: "",
  password: "",
  totp: "",
  recovery: "",
  reset_token: "",
  contact: "",
  asset_key: "",
  currency: "",
  sku: "",
  optional_sku: "",
  opaque_note: "",
  required_ids: [""],
  nullable_ids: [""],
  lines: [{ item_id: "", amount: "", optional_sku: "" }],
};
const c = bindCreate(createOp, createValues, () => {}, []);
sdk.commitFormFields(createState);

// Every non-optional framework codec declaring NonEmpty is required without any rule,
// including password.Plaintext and the newly covered auth/email/storage/money codecs.
for (const name of [
  "owner_id",
  "password",
  "totp",
  "recovery",
  "reset_token",
  "contact",
  "asset_key",
  "currency",
  "sku",
]) {
  assert.equal(c[name].required, true, `${name} must publish required: true`);
  assert.equal(c[name].checks.required, true, `${name} must publish checks.required: true`);
  assert.equal(createOp.field("body", name).schema?.non_empty, true, `${name} manifest schema must carry non_empty: true`);
}

// Omitted-when-zero tag, Optional wrapper and opaque application codec stay optional.
assert.equal(c.omitzero_id.required, false, "omitzero ID must withhold required");
assert.equal(c.omitzero_id.checks.required, false);
assert.equal(createOp.field("body", "omitzero_id").schema?.non_empty, true, "omitzero ID type still declares non_empty");
assert.equal(c.optional_sku.required, false, "Optional[AppSKU] must withhold required");
assert.equal(c.optional_sku.checks.required, false);
assert.equal(c.opaque_note.required, false, "application codec without NonEmpty stays required: false");
assert.equal(c.opaque_note.checks.required, false);
assert.equal(createOp.field("body", "opaque_note").schema?.non_empty, undefined);

// List rows: non-nullable value.List carries required into each row and nested line fields;
// a nullable Go slice parent withholds required on its rows.
assert.equal(c.required_ids.at(0).required, true);
assert.equal(c.required_ids.at(0).checks.required, true);
assert.equal(c.required_ids.at(0).row, true);
assert.equal(c.nullable_ids.at(0).required, false);
assert.equal(c.nullable_ids.at(0).checks.required, false);
assert.equal(c.lines.at(0).item_id.required, true);
assert.equal(c.lines.at(0).item_id.checks.required, true);
assert.equal(c.lines.at(0).amount.required, true);
assert.equal(c.lines.at(0).amount.checks.required, true);
assert.equal(c.lines.at(0).optional_sku.required, false);
assert.equal(c.lines.at(0).optional_sku.checks.required, false);

// PATCH fields follow the exact same presence chain as POST:
// non-optional field is required; Optional, Optional[Nullable], and pointer withhold required;
// Optional with an unconditional validation.Required rule IS required.
const patchState = sdk.formFieldState();
const bindPatch = sdk.fieldBinder((key) => key, () => patchState);
const patchValues = {
  owner_id: "",
  optional_owner_id: "",
  nullable_owner_id: "",
  pointer_owner_id: "",
  forced_optional_id: "",
  optional_sku: "",
};
const p = bindPatch(patchOp, patchValues, () => {}, []);
sdk.commitFormFields(patchState);

assert.equal(p.owner_id.required, true);
assert.equal(p.owner_id.checks.required, true);
assert.equal(p.optional_owner_id.required, false);
assert.equal(p.optional_owner_id.checks.required, false);
assert.equal(p.nullable_owner_id.required, false);
assert.equal(p.nullable_owner_id.checks.required, false);
assert.equal(p.pointer_owner_id.required, false);
assert.equal(p.pointer_owner_id.checks.required, false);
assert.equal(p.optional_sku.required, false);
assert.equal(p.optional_sku.checks.required, false);
assert.equal(p.forced_optional_id.required, true, "Optional field with unconditional Required rule must publish required: true");
assert.equal(p.forced_optional_id.checks.required, true);

// Required TextQuery decimal & AppSKU parameters publish non_empty: true and required: true in the manifest.
const qAmount = quoteOp.field("query", "amount");
const qSku = quoteOp.field("query", "sku");
const qOptAmount = quoteOp.field("query", "optional_amount");
assert.equal(qAmount.required, true);
assert.equal(qAmount.schema?.non_empty, true);
assert.equal(qSku.required, true);
assert.equal(qSku.schema?.non_empty, true);
assert.equal(qOptAmount.required, false);
assert.equal(qOptAmount.schema?.non_empty, true);

// Live server round-trip through the generated TypeScript client against the real Go server.
assert.ok(baseURL, "live server URL must be set");
const client = sdk.createClient(sdk.fetchTransport, { baseURL });
await quoteOp.call(client, { query: { amount: "12.50", sku: "SKU-42" } });
await patchOp.call(client, { body: { owner_id: validID, pointer_owner_id: null, forced_optional_id: validID } });

// Model ID detection and fieldIdentity on real generated Go model.ID[T] DTOs,
// including sending the parsed ID to the live Go server.
for (const [label, binding] of [
  ["create.owner_id", c.owner_id],
  ["create.omitzero_id", c.omitzero_id],
  ["create.required_ids.at(0)", c.required_ids.at(0)],
  ["create.nullable_ids.at(0)", c.nullable_ids.at(0)],
  ["create.lines.at(0).item_id", c.lines.at(0).item_id],
  ["patch.owner_id", p.owner_id],
  ["patch.optional_owner_id", p.optional_owner_id],
  ["patch.nullable_owner_id", p.nullable_owner_id],
  ["patch.pointer_owner_id", p.pointer_owner_id],
  ["patch.forced_optional_id", p.forced_optional_id],
]) {
  assert.equal(binding.identity, true, `${label} must report identity: true`);
}
for (const [label, binding] of [
  ["create.password", c.password],
  ["create.totp", c.totp],
  ["create.contact", c.contact],
  ["create.sku", c.sku],
  ["create.opaque_note", c.opaque_note],
  ["create.required_ids", c.required_ids],
  ["create.nullable_ids", c.nullable_ids],
  ["create.lines.at(0).amount", c.lines.at(0).amount],
]) {
  assert.equal(binding.identity, false, `${label} must report identity: false`);
  assert.equal(sdk.fieldIdentity(binding, validID), undefined, `non-ID ${label} must return undefined`);
}
const enteredOwner = sdk.fieldIdentity(p.owner_id, validID);
const enteredRow = sdk.fieldIdentity(c.required_ids.at(0), "0193FD8C-2075-7000-8000-000000000002");
assert.equal(enteredOwner, validID);
assert.equal(enteredRow, "0193FD8C-2075-7000-8000-000000000002");
assert.equal(sdk.fieldIdentity(p.owner_id, ""), undefined);
assert.equal(sdk.fieldIdentity(p.owner_id, "not-a-uuid"), undefined);
assert.equal(sdk.fieldIdentity(p.owner_id, "00000000-0000-0000-0000-000000000000"), undefined);
await patchOp.call(client, { body: { owner_id: enteredOwner, pointer_owner_id: null, forced_optional_id: enteredRow } });
