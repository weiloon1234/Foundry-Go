import * as sdk from "./contracts_foundry.gen.js";

type CreateBody = sdk.Operations["codecCreate"]["request"]["body"];
type CreateDraft = Required<sdk.FormValues<CreateBody>>;
declare const createDraft: CreateDraft;
const createUpdate: sdk.FormUpdate<CreateDraft> = () => {};

type PatchBody = sdk.Operations["codecPatch"]["request"]["body"];
type PatchDraft = Required<sdk.FormValues<PatchBody>>;
declare const patchDraft: PatchDraft;
const patchUpdate: sdk.FormUpdate<PatchDraft> = () => {};

const createOp = sdk.operation("codecCreate");
const patchOp = sdk.operation("codecPatch");
const quoteOp = sdk.operation("codecQuote");

const createState: sdk.FormFieldState = sdk.formFieldState();
const patchState: sdk.FormFieldState = sdk.formFieldState();
const bindCreate = sdk.fieldBinder((key) => key, () => createState);
const bindPatch = sdk.fieldBinder((key) => key, () => patchState);

const c = bindCreate(createOp, createDraft, createUpdate, []);
const p = bindPatch(patchOp, patchDraft, patchUpdate, []);

const owner: sdk.FieldBinding<sdk.UserID> = c.owner_id;
const omitzeroID: sdk.FieldBinding<sdk.UserID> = c.omitzero_id;
const password: sdk.FieldBinding<string> = c.password;
const totp: sdk.FieldBinding<string> = c.totp;
const recovery: sdk.FieldBinding<string> = c.recovery;
const resetToken: sdk.FieldBinding<string> = c.reset_token;
const contact: sdk.FieldBinding<string> = c.contact;
const assetKey: sdk.FieldBinding<string> = c.asset_key;
const currency: sdk.FieldBinding<string> = c.currency;
const sku: sdk.FieldBinding<string> = c.sku;
const optSku: sdk.FieldBinding<string> = c.optional_sku;
const opaqueNote: sdk.FieldBinding<string> = c.opaque_note;
const requiredRowID: sdk.FieldBinding<sdk.UserID> = c.required_ids.at(0);
const nullableRowID: sdk.FieldBinding<sdk.UserID> = c.nullable_ids.at(0);
const lineItemID: sdk.FieldBinding<sdk.OrderID> = c.lines.at(0).item_id;
const lineAmount: sdk.FieldBinding<string> = c.lines.at(0).amount;
const lineOptSku: sdk.FieldBinding<string | undefined> = c.lines.at(0).optional_sku;

const patchOwner: sdk.FieldBinding<sdk.UserID> = p.owner_id;
const patchOpt: sdk.FieldBinding<sdk.UserID> = p.optional_owner_id;
const patchNull: sdk.FieldBinding<sdk.UserID> = p.nullable_owner_id;
const patchPtr: sdk.FieldBinding<sdk.UserID> = p.pointer_owner_id;
const patchForced: sdk.FieldBinding<sdk.UserID> = p.forced_optional_id;
const patchSku: sdk.FieldBinding<string> = p.optional_sku;

const queryAmount: sdk.FieldDescriptor<string, readonly ["codecQuote", "query", "amount"]> = quoteOp.field("query", "amount");
const querySku: sdk.FieldDescriptor<string, readonly ["codecQuote", "query", "sku"]> = quoteOp.field("query", "sku");
const queryOptAmount: sdk.FieldDescriptor<string | undefined, readonly ["codecQuote", "query", "optional_amount"]> = quoteOp.field("query", "optional_amount");

// A row object in a real generated Go DTO (CodecFormCreate -> CodecFormLine)
// using the standard nested FormValues spelling: item_id is omitted in the
// initial values under exactOptionalPropertyTypes, binds as
// FieldBinding<OrderID | undefined>, strict siblings keep FieldBinding<V>, and
// request building rejects an unnarrowed item_id.
type Line = NonNullable<CreateBody["lines"]>[number];
type UnsetLineCreateDraft = Required<Omit<sdk.FormValues<CreateBody>, "lines">> & {
  readonly lines: readonly (Required<Omit<sdk.FormValues<Line>, "item_id">> & Pick<sdk.FormValues<Line>, "item_id">)[];
};
const unsetLineDraft: Pick<UnsetLineCreateDraft, "lines"> = {
  lines: [{ amount: "9.95", optional_sku: "" }],
};
const unsetLines = bindCreate(createOp, unsetLineDraft, () => {}, []);
const unsetLineItemID: sdk.FieldBinding<sdk.OrderID | undefined> = unsetLines.lines.at(0).item_id;
// @ts-expect-error An unset row ID cannot bind as always-present.
const strictLineItemID: sdk.FieldBinding<sdk.OrderID> = unsetLines.lines.at(0).item_id;
const strictLineAmount: sdk.FieldBinding<string> = unsetLines.lines.at(0).amount;
// @ts-expect-error A strict sibling row leaf stays FieldBinding<string>.
const widenedLineAmount: sdk.FieldBinding<string | undefined> = unsetLines.lines.at(0).amount;
function buildCreateLineUnnarrowed(row: UnsetLineCreateDraft["lines"][number]): Line {
  return {
    // @ts-expect-error An unset row ID cannot flow into a required request field without narrowing.
    item_id: row.item_id,
    amount: row.amount,
  };
}
function buildCreateLineNarrowed(row: UnsetLineCreateDraft["lines"][number]): Line | undefined {
  if (row.item_id === undefined) return undefined;
  return { item_id: row.item_id, amount: row.amount };
}

void [
  owner, omitzeroID, password, totp, recovery, resetToken, contact, assetKey, currency,
  sku, optSku, opaqueNote, requiredRowID, nullableRowID, lineItemID, lineAmount, lineOptSku,
  patchOwner, patchOpt, patchNull, patchPtr, patchForced, patchSku,
  queryAmount, querySku, queryOptAmount,
  unsetLineItemID, strictLineItemID, strictLineAmount, widenedLineAmount, buildCreateLineUnnarrowed, buildCreateLineNarrowed,
];
