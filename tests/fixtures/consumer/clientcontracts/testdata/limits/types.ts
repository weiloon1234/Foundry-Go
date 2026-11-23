import * as sdk from "./contracts_foundry.gen.js";

// The binder is typed from the request: a field exists only if the request
// declares it with a Go label, and each binding's value is the form's value.
type Body = sdk.Operations["itemsEcho"]["request"]["body"];
type Draft = Required<sdk.FormValues<Body>>;
declare const draft: Draft;
const update: sdk.FormUpdate<Draft> = () => {};
const op = sdk.operation("itemsEcho");
const state: sdk.FormFieldState = sdk.formFieldState();
const bind = sdk.fieldBinder((key) => key, () => state);
const f = bind(op, draft, update, []);
const text: sdk.FieldBinding<string> = f.text;
const count: sdk.FieldBinding<number> = f.count;
const entry: sdk.FieldBinding<string> = f.names.at("en");
// A row of a list binds like a field of the list's element type.
const code: sdk.FieldBinding<string> = f.codes.at(0);
const checks: sdk.FieldChecks = f.text.checks;
// A boolean binds as a boolean; a float as a number.
const consent: sdk.FieldBinding<boolean> = f.consent;
const share: sdk.FieldBinding<number> = f.share;
// Binding computes; the form publishes the render it shows.
sdk.commitFormFields(state);
// @ts-expect-error Only a form's binder state is committed.
sdk.commitFormFields({});
// @ts-expect-error A field without a Go label has no binding.
void f.unlabelled;
// @ts-expect-error Only the binder creates a binding; an object literal is not one.
const forged: sdk.FieldBinding<string> = { name: "text", label: "Text" };
// @ts-expect-error A spread of a real binding is not a binding.
const spread: sdk.FieldBinding<string> = { ...f.text };
// @ts-expect-error Nor is a copy with one member replaced.
const copied: sdk.FieldBinding<string> = { ...f.text, limits: {} };
// @ts-expect-error The class is exported as a type only: no value to construct.
new sdk.FieldBinding<string>();
// @ts-expect-error Nor a prototype to create one from.
const created: sdk.FieldBinding<string> = Object.create(sdk.FieldBinding.prototype);
// @ts-expect-error A form's binder state comes from formFieldState().
sdk.fieldBinder((key) => key, () => ({}));
// @ts-expect-error A form value of the wrong type for its request field is rejected.
bind(op, { text: 1 }, () => {}, []);
// @ts-expect-error A plain value has no entries to bind.
f.text.at(0);
// A form that submits several operations binds them together and receives one
// set of bindings per operation, in order, typed by each request.
const [items, generic] = bind([op, sdk.operation("genericEcho")], draft, update, []);
const fromItems: sdk.FieldBinding<string> = items.text;
const fromGeneric: sdk.FieldBinding<number> = generic.count;
// @ts-expect-error One operation is bound without brackets.
bind([op], draft, update, []);
// @ts-expect-error The form's values must fit every operation's request.
bind([op, sdk.operation("genericEcho")], { text: 1 }, () => {}, []);
// A nested object binds as its labelled fields, in the same call.
const username: sdk.FieldBinding<string> = f.account.username;
const email: sdk.FieldBinding<string> = f.account.email;
// @ts-expect-error A nested field without a Go label has no binding.
void f.account.internal;
// @ts-expect-error A nested object is not a binding itself: its fields are.
void f.account.value;
// An optional, nullable nested object: the form holds its values and decides
// when building the request whether to send it.
const color: sdk.FieldBinding<string> = f.prefs.color;
function prefsBody(values: Draft): Body["prefs"] {
  return values.prefs.color.trim() ? { color: values.prefs.color, owner_id: values.prefs.owner_id } : null;
}
// Rows of fields, and a list inside each row.
const rowName: sdk.FieldBinding<string> = f.rows.at(0).name;
const opens: sdk.FieldBinding<string> = f.rows.at(0).slots.at(1).opens;
const rows: sdk.FieldBinding<Draft["rows"]> = f.rows;
// @ts-expect-error A row of fields is not a binding itself.
void f.rows.at(0).issues;
// A map keyed by an enum binds an entry per key of the enum.
const stateEntry: sdk.FieldBinding<string> = f.states.at("open");
// @ts-expect-error A key the enum does not declare.
f.states.at("other");
// A component that shows some fields of a row takes them as a plain type.
function SlotFields(props: { readonly fields: { readonly opens: sdk.FieldBinding<string> } }): string {
  return props.fields.opens.name;
}
SlotFields({ fields: f.rows.at(0).slots.at(0) });
// Rows: the form adds, removes and moves entries through its own update, and
// binds each with at(i).
const addRow = () => update((values) => ({ ...values, codes: [...values.codes, ""] }));
const removeRow = (index: number) => update((values) => ({ ...values, codes: values.codes.filter((_, at) => at !== index) }));
const addSlot = (row: number) => update((values) => ({ ...values, rows: values.rows.map((each, at) => (at === row ? { ...each, slots: [...each.slots, { opens: "" }] } : each)) }));
const row: sdk.FieldBinding<string> = f.codes.at(1);
// A second list of the same rows binds its own rows.
const moreName: sdk.FieldBinding<string> = f.more_rows.at(0).name;
// A list the form's values may leave out has no rows to bind: the draft holds
// the list (Required<FormValues<T>> holds every top-level field).
declare const loose: sdk.FormValues<Body>;
// @ts-expect-error A list the values may leave out has no at().
bind(op, loose, () => {}, []).codes.at(0);
// A nested object and a list of objects bind without a label of their own;
// a field below them still needs its Go label.
const address: sdk.FieldBinding<string> = f.wallet.address;
// @ts-expect-error A field without a Go label has no binding.
void f.wallet.note;
const role: sdk.FieldBinding<string> = f.links.at(0).role;
// @ts-expect-error A list without a label of its own binds by row only.
void f.links.value;
// A self-referencing object binds as far as its type does.
const filterName: sdk.FieldBinding<string> = f.filter.name;
// @ts-expect-error The binder stops where the type repeats.
void f.filter.not.name;
// A rule that must hold however the values change lives in the form's update
// function: a row whose name is cleared loses its slots, whichever code
// cleared it.
const keepRows: sdk.FormUpdate<Draft> = (updater) => update((values) => {
  const next = updater(values);
  return { ...next, rows: next.rows.map((each) => (each.name.trim() ? each : { ...each, slots: [] })) };
});
bind(op, draft, keepRows, []);
// A create and an update whose bodies differ in shape bind in one call; a
// child shows the fields of the operation the form submits.
type AccountDraft = Draft & Required<sdk.FormValues<sdk.Operations["membersUpsert"]["request"]["body"]>>;
declare const accountDraft: AccountDraft;
declare const editing: boolean;
const [creating, updating] = bind([op, sdk.operation("membersUpsert")], accountDraft, () => {}, []);
function AccountFields(props: { readonly fields: { readonly username: sdk.FieldBinding<string> } }): string {
  return props.fields.username.name;
}
AccountFields({ fields: editing ? updating : creating.account });
// An upload may be left out of a form's values until a file is chosen.
type UploadBody = sdk.Operations["uploadsProfile"]["request"]["body"];
type UploadDraft = Required<Omit<sdk.FormValues<UploadBody>, "document">> & Pick<sdk.FormValues<UploadBody>, "document">;
declare const uploadDraft: UploadDraft;
const up = bind(sdk.operation("uploadsProfile"), uploadDraft, () => {}, []);
const document: sdk.FieldBinding<sdk.Upload | undefined> = up.document;
// Repeated parts bind a row per upload or text; a JSON part binds its fields.
const photo: sdk.FieldBinding<sdk.Upload> = up.photos.at(0);
// A repeated upload is a row: its binding is not one of an optional upload,
// whose control may clear it.
// @ts-expect-error A row's change function never receives undefined.
const clearable: sdk.FieldBinding<sdk.Upload | undefined> = up.photos.at(0);
const isRow: boolean = photo.row;
const tag: sdk.FieldBinding<string> = up["tags[]"].at(0);
const caption: sdk.FieldBinding<string> = up.settings.caption;
// A multipart body is built from the form's values like a JSON one, once the
// user has chosen the file.
function uploadBody(values: UploadDraft): UploadBody | undefined {
  return values.document && { document: values.document, title: values.title, photos: values.photos, settings: { caption: values.settings.caption } };
}
const key: sdk.FieldMessageKey = "help.text";
// @ts-expect-error A key no request field declares is not a field message key.
const missing: sdk.FieldMessageKey = "fields.nowhere";
const issue: sdk.Issue = sdk.issueAt(op.field("body", "text"), "taken", "Taken");
const ownerId: sdk.FieldBinding<sdk.Identity<"client.OwnerID">> = f.owner_id;
const pricePlaces: number | undefined = f.price.limits.maxPlaces;
const checkedPlaces: number | undefined = f.price.checks.limits.maxPlaces;
const malformed: boolean | undefined = f.price.checks.malformed;
const parsedPlaces: number | undefined = sdk.decimalPlaces("3.001");

// doc-example:begin form-unset-nested
export function formUnsetNested(issues: readonly sdk.Issue[]) {
  type FormBody = sdk.Operations["itemsEcho"]["request"]["body"];
  type Prefs = NonNullable<FormBody["prefs"]>;
  type Row = NonNullable<FormBody["rows"]>[number];
  type UnsetDraft = Required<Omit<sdk.FormValues<FormBody>, "prefs" | "rows">> & {
    readonly prefs: Required<Omit<sdk.FormValues<Prefs>, "owner_id">> & Pick<sdk.FormValues<Prefs>, "owner_id">;
    readonly rows: readonly (Required<Omit<sdk.FormValues<Row>, "name">> & Pick<sdk.FormValues<Row>, "name">)[];
  };
  let values: Pick<UnsetDraft, "prefs" | "rows"> = {
    prefs: { color: "blue" },
    rows: [{ slots: [{ opens: "0900" }] }],
  };
  const update: sdk.FormUpdate<typeof values> = (next) => { values = next(values); };
  const form = sdk.formFieldState();
  const fields = sdk.fieldBinder((key) => key, () => form);
  const bound = fields(sdk.operation("itemsEcho"), values, update, issues);
  const owner: sdk.FieldBinding<sdk.Identity<"client.OwnerID"> | undefined> = bound.prefs.owner_id;
  const strictColor: sdk.FieldBinding<string> = bound.prefs.color;
  const rowTitle: sdk.FieldBinding<string | undefined> = bound.rows.at(0).name;
  const slotOpens: sdk.FieldBinding<string> = bound.rows.at(0).slots.at(0).opens;
  const toPrefs = (): Prefs | undefined => values.prefs.owner_id === undefined
    ? undefined
    : { color: values.prefs.color, owner_id: values.prefs.owner_id };
  return { owner, strictColor, rowTitle, slotOpens, toPrefs };
}
// doc-example:end form-unset-nested

// Exact spelling on a real generated multipart DTO with a nested object
// (httpuploads.ProfileInput -> Settings) and request-builder narrowing check.
type UploadSettings = NonNullable<UploadBody["settings"]>;
type UnsetSettingsDraft = Required<Omit<sdk.FormValues<UploadBody>, "document" | "settings">>
  & Pick<sdk.FormValues<UploadBody>, "document"> & {
    readonly settings: Required<Omit<sdk.FormValues<UploadSettings>, "caption">> & Pick<sdk.FormValues<UploadSettings>, "caption">;
  };
const unsetSettingsDraft: Pick<UnsetSettingsDraft, "tags[]" | "settings"> = { "tags[]": ["cover"], settings: { note: "" } };
const upUnset = bind(sdk.operation("uploadsProfile"), unsetSettingsDraft, () => {}, []);
const unsetCaption: sdk.FieldBinding<string | undefined> = upUnset.settings.caption;
const strictUploadTag: sdk.FieldBinding<string> = upUnset["tags[]"].at(0);
// @ts-expect-error A strict sibling stays FieldBinding<string>.
const widenedUploadTag: sdk.FieldBinding<string | undefined> = upUnset["tags[]"].at(0);
function buildSettingsWithoutNarrowing(values: Pick<UnsetSettingsDraft, "settings">): UploadSettings {
  return {
    // @ts-expect-error An optional nested leaf must be narrowed before flowing into a required request field.
    caption: values.settings.caption,
  };
}

// The same spelling for a nested reference ID and a row leaf: strict siblings
// stay FieldBinding<V>, unset leaves bind as FieldBinding<V | undefined>, and
// request building rejects an unnarrowed unset leaf.
type OwnerID = sdk.Identity<"client.OwnerID">;
type Prefs = NonNullable<Body["prefs"]>;
type Row = NonNullable<Body["rows"]>[number];
type IdiomDraft = Required<Omit<sdk.FormValues<Body>, "prefs" | "rows">> & {
  readonly prefs: Required<Omit<sdk.FormValues<Prefs>, "owner_id">> & Pick<sdk.FormValues<Prefs>, "owner_id">;
  readonly rows: readonly (Required<Omit<sdk.FormValues<Row>, "name">> & Pick<sdk.FormValues<Row>, "name">)[];
};
declare const idiomDraft: IdiomDraft;
const idf = bind(op, idiomDraft, () => {}, []);
const idiomOwner: sdk.FieldBinding<OwnerID | undefined> = idf.prefs.owner_id;
const idiomColor: sdk.FieldBinding<string> = idf.prefs.color;
// @ts-expect-error A nested leaf always present in the idiom draft stays strict.
const idiomColorLoose: sdk.FieldBinding<string | undefined> = idf.prefs.color;
// @ts-expect-error An unset nested leaf in the idiom draft cannot bind as always-present.
const idiomOwnerStrict: sdk.FieldBinding<OwnerID> = idf.prefs.owner_id;
const idiomRowName: sdk.FieldBinding<string | undefined> = idf.rows.at(0).name;
const idiomSlotOpens: sdk.FieldBinding<string> = idf.rows.at(0).slots.at(0).opens;
// @ts-expect-error An unset row leaf cannot bind as always-present.
const idiomRowNameStrict: sdk.FieldBinding<string> = idf.rows.at(0).name;
// @ts-expect-error A strict nested-row leaf does not widen to undefined.
const idiomSlotOpensLoose: sdk.FieldBinding<string | undefined> = idf.rows.at(0).slots.at(0).opens;
function buildIdiomPrefs(values: IdiomDraft): Prefs {
  return {
    color: values.prefs.color,
    // @ts-expect-error An unset nested leaf cannot flow into a required request field without narrowing.
    owner_id: values.prefs.owner_id,
  };
}
function buildIdiomRow(row: IdiomDraft["rows"][number]): Row {
  return {
    // @ts-expect-error An unset row leaf cannot flow into a required request field without narrowing.
    name: row.name,
    slots: row.slots,
  };
}

// Explicit undefined in an inline literal (probe shape) and select-like consumer.
const probeNestedUnset = { prefs: { color: "blue", owner_id: undefined } };
bind(op, probeNestedUnset, () => {}, []);

type NestedSelectDraft = {
  readonly prefs: {
    readonly color: string;
    readonly owner_id: OwnerID | undefined;
  };
};
const nestedSelectDraft: NestedSelectDraft = { prefs: { color: "blue", owner_id: undefined } };
const ns = bind(op, nestedSelectDraft, () => {}, []);
function OwnerSelect(props: { readonly field: sdk.FieldBinding<OwnerID | undefined> }): string {
  return props.field.name;
}
const selectedOwnerName: string = OwnerSelect({ field: ns.prefs.owner_id });
const strictNestedColor: sdk.FieldBinding<string> = ns.prefs.color;
// @ts-expect-error A nested leaf always present in values does not widen to undefined.
const widenedNestedColor: sdk.FieldBinding<string | undefined> = ns.prefs.color;
// @ts-expect-error A nested leaf that may be undefined does not satisfy a non-undefined consumer.
const strictNestedOwner: sdk.FieldBinding<OwnerID> = ns.prefs.owner_id;

function buildPrefsFromDraft(values: NestedSelectDraft): NonNullable<Body["prefs"]> {
  return {
    color: values.prefs.color,
    // @ts-expect-error An unset form value is not assignable to a required request field.
    owner_id: values.prefs.owner_id,
  };
}

// Strict versus unset row leaves side by side, plus an absent nested object
// inside a row.
type PartialRowDraft = {
  readonly rows: readonly {
    readonly name: string;
    readonly slots: readonly { readonly opens?: string | undefined }[];
  }[];
  readonly more_rows: readonly {
    readonly name?: string | undefined;
    readonly slots: readonly { readonly opens: string }[];
  }[];
  readonly links: readonly {
    readonly role: string;
    readonly meta?: { readonly note: string } | undefined;
  }[];
};
const partialRowDraft: PartialRowDraft = {
  rows: [{ name: "shift", slots: [{}] }],
  more_rows: [{ name: undefined, slots: [{ opens: "0900" }] }],
  links: [{ role: "owner" }],
};
const pr = bind(op, partialRowDraft, () => {}, []);
const strictRowName: sdk.FieldBinding<string> = pr.rows.at(0).name;
// @ts-expect-error A strict row leaf does not widen to undefined.
const widenedRowName: sdk.FieldBinding<string | undefined> = pr.rows.at(0).name;
const unsetSlotOpens: sdk.FieldBinding<string | undefined> = pr.rows.at(0).slots.at(0).opens;
// @ts-expect-error An unset nested-row leaf cannot bind as always-present.
const narrowedSlotOpens: sdk.FieldBinding<string> = pr.rows.at(0).slots.at(0).opens;
const unsetRowMetaNote: sdk.FieldBinding<string | undefined> = pr.links.at(0).meta.note;
// @ts-expect-error A leaf under an optional nested object inside a row may be undefined.
const strictRowMetaNote: sdk.FieldBinding<string> = pr.links.at(0).meta.note;
const unsetRowName: sdk.FieldBinding<string | undefined> = pr.more_rows.at(0).name;
// @ts-expect-error An unset row leaf cannot bind as always-present.
const narrowedRowName: sdk.FieldBinding<string> = pr.more_rows.at(0).name;
const strictSlotOpens: sdk.FieldBinding<string> = pr.more_rows.at(0).slots.at(0).opens;
// @ts-expect-error A strict nested-row leaf does not widen to undefined.
const widenedSlotOpens: sdk.FieldBinding<string | undefined> = pr.more_rows.at(0).slots.at(0).opens;

// Absent nested object (one level and two levels deep), nested map under an
// absent parent, and an inherited-key property name ("toString").
type AbsentPrefsDraft = {
  readonly text: string;
  readonly prefs?: {
    readonly color: string;
    readonly owner_id?: OwnerID | undefined;
  } | undefined;
  readonly wallet?: {
    readonly address: string;
    readonly detail?: { readonly leaf: string } | undefined;
    readonly labels?: Record<string, string> | undefined;
    readonly toString?: string | undefined;
  } | undefined;
};
let absentPrefsDraft: AbsentPrefsDraft = { text: "keep" };
const ap = bind(op, absentPrefsDraft, (next) => { absentPrefsDraft = next(absentPrefsDraft); }, []);
const absentPrefsColor: sdk.FieldBinding<string | undefined> = ap.prefs.color;
const absentPrefsOwner: sdk.FieldBinding<OwnerID | undefined> = ap.prefs.owner_id;
// @ts-expect-error A leaf under an optional nested object may be undefined.
const strictAbsentColor: sdk.FieldBinding<string> = ap.prefs.color;
const twoLevelLeaf: sdk.FieldBinding<string | undefined> = ap.wallet.detail.leaf;
// @ts-expect-error A leaf two levels below an absent nested object may be undefined.
const strictTwoLevelLeaf: sdk.FieldBinding<string> = ap.wallet.detail.leaf;
const absentParentMapEntry: sdk.FieldBinding<string> = ap.wallet.labels.at("en");
const inheritedKeyField: sdk.FieldBinding<string | undefined> = ap.wallet.toString;
// An optional nested object typed strictly as undefined binds no fields.
const onlyUndefinedPrefs = bind(op, { prefs: undefined }, () => {}, []);
// @ts-expect-error No nested keys are declared when the object's type is undefined alone.
void onlyUndefinedPrefs.prefs.color;
// A labelled map typed strictly as undefined does not expose at() (never FieldBinding<never>).
const onlyUndefinedNames = bind(op, { names: undefined }, () => {}, []);
const onlyUndefinedNamesWhole: sdk.FieldBinding<undefined> = onlyUndefinedNames.names;
// @ts-expect-error A map whose value type is strictly undefined has no at().
onlyUndefinedNames.names.at("en");
// An optional map whose value type still names the map keeps at() typed as string.
declare const optionalNamesDraft: { readonly names?: Record<string, string> | undefined };
const optionalNamesEntry: sdk.FieldBinding<string> = bind(op, optionalNamesDraft, () => {}, []).names.at("en");

// Multi-operation binding also accepts unset nested fields across operations.
const [multiItems, multiUpdate] = bind(
  [op, sdk.operation("membersUpsert")],
  { account: { username: "ann", email: undefined }, username: "ann", email: undefined },
  () => {},
  [],
);
const multiAccountUsername: sdk.FieldBinding<string> = multiItems.account.username;
// @ts-expect-error A field of another operation is still absent from membersUpsert bindings.
void multiUpdate.prefs;

// Generic V accepts wider drafts carrying an extra nested or row key beside a
// valid sibling (needed for multi-operation drafts); BoundFields leaves the
// unknown key unbound.
const extraNested = bind(op, { prefs: { color: "blue", missing: "x" } }, () => {}, []);
const extraNestedColor: sdk.FieldBinding<string> = extraNested.prefs.color;
// @ts-expect-error An unknown nested key carried alongside a valid sibling has no binding.
void extraNested.prefs.missing;
const extraRow = bind(op, { rows: [{ name: "a", missing: "x" }] }, () => {}, []);
const extraRowName: sdk.FieldBinding<string> = extraRow.rows.at(0).name;
// @ts-expect-error An unknown row key carried alongside a valid sibling has no binding.
void extraRow.rows.at(0).missing;

// Negative checks: unknown-only nested objects (weak-type check), wrong types,
// null parents/leaves, missing rows and unlabelled nested leaves fail to compile.
// @ts-expect-error A nested field name not in the request has no binding.
void ns.prefs.missing;
// @ts-expect-error A row field name not in the request has no binding.
void pr.rows.at(0).missing;
// @ts-expect-error A nested object with no declared keys fails weak-type checking.
bind(op, { prefs: { missing: "x" } }, () => {}, []);
// @ts-expect-error A nested value of the wrong type is rejected.
bind(op, { prefs: { owner_id: 123 } }, () => {}, []);
// @ts-expect-error A plain string is not assignable to a nested branded ID.
bind(op, { prefs: { owner_id: "0193fd8c-2075-7000-8000-000000000001" } }, () => {}, []);
// @ts-expect-error Null is not a form value for a nested object.
bind(op, { prefs: null }, () => {}, []);
// @ts-expect-error Null is not a form value for a nested field.
bind(op, { prefs: { color: null } }, () => {}, []);
// @ts-expect-error A row field of the wrong type is rejected.
bind(op, { rows: [{ name: 42 }] }, () => {}, []);
// @ts-expect-error A row object itself cannot be undefined.
bind(op, { rows: [undefined] }, () => {}, []);
// @ts-expect-error A row of plain values cannot be undefined.
bind(op, { codes: [undefined] }, () => {}, []);
// @ts-expect-error An unlabelled nested leaf still has no binding even when unset.
void bind(op, { account: { username: "ann", internal: undefined } }, () => {}, []).account.internal;

void [text, count, entry, code, checks, consent, share, clearable, isRow, username, email, color, prefsBody, rowName, opens, rows, stateEntry, document, photo, tag, caption, uploadBody, addRow, removeRow, addSlot, row, moreName, address, role, filterName, forged, spread, copied, created, fromItems, fromGeneric, key, missing, issue, ownerId, pricePlaces, checkedPlaces, malformed, parsedPlaces, unsetCaption, strictUploadTag, widenedUploadTag, buildSettingsWithoutNarrowing, idiomOwner, idiomColor, idiomColorLoose, idiomOwnerStrict, idiomRowName, idiomSlotOpens, idiomRowNameStrict, idiomSlotOpensLoose, buildIdiomPrefs, buildIdiomRow, selectedOwnerName, strictNestedColor, widenedNestedColor, strictNestedOwner, buildPrefsFromDraft, strictRowName, widenedRowName, unsetSlotOpens, narrowedSlotOpens, unsetRowMetaNote, strictRowMetaNote, unsetRowName, narrowedRowName, strictSlotOpens, widenedSlotOpens, absentPrefsColor, absentPrefsOwner, strictAbsentColor, twoLevelLeaf, strictTwoLevelLeaf, absentParentMapEntry, inheritedKeyField, onlyUndefinedNamesWhole, optionalNamesEntry, multiAccountUsername, extraNestedColor, extraRowName];
