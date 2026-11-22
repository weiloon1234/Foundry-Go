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

void [
  owner, omitzeroID, password, totp, recovery, resetToken, contact, assetKey, currency,
  sku, optSku, opaqueNote, requiredRowID, nullableRowID, lineItemID, lineAmount, lineOptSku,
  patchOwner, patchOpt, patchNull, patchPtr, patchForced, patchSku,
  queryAmount, querySku, queryOptAmount,
];
