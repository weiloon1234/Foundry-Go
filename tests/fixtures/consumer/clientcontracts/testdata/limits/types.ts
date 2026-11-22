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
void [text, count, entry, code, checks, consent, share, clearable, isRow, username, email, color, prefsBody, rowName, opens, rows, stateEntry, document, photo, tag, caption, uploadBody, addRow, removeRow, addSlot, row, moreName, address, role, filterName, forged, spread, copied, created, fromItems, fromGeneric, key, missing, issue, ownerId, pricePlaces, checkedPlaces, malformed, parsedPlaces];
