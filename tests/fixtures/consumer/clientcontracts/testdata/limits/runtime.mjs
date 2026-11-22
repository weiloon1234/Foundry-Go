import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import v8 from "node:v8";
import vm from "node:vm";
const sdk = await import(pathToFileURL(process.argv[2]));
const op = sdk.operation("itemsEcho");
const field = (name) => op.field("body", name);
const plain = (value) => JSON.parse(JSON.stringify(value));

// Limits come from built-in rules only, in Go's units; transformed values and
// conditional rules publish nothing; presence, non-empty and bounds stay apart.
assert.deepEqual(plain(field("text").limits), { nonEmpty: true, minLength: 3, maxLength: 10, maxBytes: 30 });
assert.deepEqual(plain(field("optional_text").limits), { nonEmpty: true, minLength: 2 });
assert.equal(field("optional_text").required, false);
assert.deepEqual(plain(field("nullable_text").limits), { nonEmpty: true });
assert.equal(field("nullable_text").nullable, true);
assert.deepEqual(plain(field("transformed").limits), {});
assert.deepEqual(plain(field("guarded").limits), {});
assert.deepEqual(plain(field("count").limits), { min: 1, max: 10, step: 1 });
assert.deepEqual(plain(field("ratio").limits), { min: "0.5", step: "any" }, "a fractional bound stays exact text");
assert.deepEqual(plain(field("price").limits), { min: "0.01", step: "0.01" });
assert.deepEqual(plain(field("names").limits), {});
assert.deepEqual(plain(field("names").at("en").limits), { nonEmpty: true, maxBytes: 12 });
assert.equal(field("names").at("en").required, true);
assert.deepEqual(plain(field("names").at("ms").limits), { maxBytes: 12 });
assert.equal(field("names").at("ms").required, false);
assert.deepEqual(plain(field("codes").limits), {}, "item counts are checked on submit, not published");
assert.deepEqual(plain(field("codes").at(0).limits), { maxLength: 4 });
assert.equal(field("names").at("en").presentation.label_key, "fields.names", "an entry is labelled by its collection");
assert.deepEqual(plain(field("min_text").limits), { minLength: 2 }, "a length minimum is a bound, not non-empty");
assert.deepEqual(plain(field("secret").limits), { minLength: 1, maxLength: 64 });
assert.deepEqual(plain(field("supplied").limits), { nonEmpty: true, maxLength: 8 });
assert.deepEqual(plain(field("optional_names").at("en").limits), { nonEmpty: true }, "a supplied map must hold the default locale");
assert.deepEqual(plain(field("share").limits), { min: 0, max: 1, step: "any" }, "a float's step lets every value Go accepts be chosen");
assert.deepEqual(plain(field("pin").limits), { minLength: 6, maxLength: 6 }, "a fixed length is Go's, published for the code's cells");

// The validator counts code points for lengths and UTF-8 bytes for byte limits.
const body = { text: "abc", optional_text: "ab", nullable_text: null, transformed: "x", guarded: "x", count: 2, ratio: 1, price: "1.00", names: { en: "hello" }, codes: ["ab"], min_text: "ab", secret: "x", choice: "a", supplied: "s", unlabelled: "x", account: { username: "ann", email: null, internal: "x" }, rows: [{ name: "a", slots: [{ opens: "0900" }] }], states: { open: "yes" }, consent: true, agreed: true, refused: false, share: 0.5, pin: "123456", starts: "2026-01-01T00:00:00Z" };
const codes = (patch) => op.validate({ path: { key: "1" }, body: { ...body, ...patch } }).issues.map((issue) => `${issue.path} ${issue.code}`);
assert.deepEqual(codes({ text: "€".repeat(10) }), [], "ten code points of 30 bytes fit both bounds");
assert.deepEqual(codes({ text: "😀".repeat(10) }), ["/body/text foundry.max_bytes"], "ten emoji are ten code points (twenty UTF-16 units) but 40 bytes");
assert.deepEqual(codes({ text: "abcdefghijk" }), ["/body/text foundry.max_length"]);
assert.deepEqual(codes({ rows: [{ name: "", slots: [{ opens: "9" }] }] }), ["/body/rows/0/name foundry.non_blank", "/body/rows/0/slots/0/opens foundry.min_length"], "row rules reach every row");

// Issues are matched by the descriptor's own escaped pointer, never by prefix text.
const issues = [
  { path: "/body/names/x~1y", code: "a" },
  { path: "/body/names/x~1yz", code: "b" },
  { path: "/body/names/x~1y/sub", code: "c" },
  { path: "/body/text", code: "d" },
  { path: "/body/textual", code: "e" },
];
assert.deepEqual(sdk.fieldIssues(field("names").at("x/y"), issues).map((issue) => issue.code), ["a", "c"]);
assert.deepEqual(sdk.fieldIssues(field("text"), issues).map((issue) => issue.code), ["d"]);
assert.deepEqual(sdk.fieldIssues(field("codes").element(), [{ path: "/body/codes/0", code: "f" }]), [], "element() is a template and addresses nothing");
assert.deepEqual(plain(sdk.issueAt(field("text"), "taken", "Taken")), { path: "/body/text", code: "taken", message: "Taken" });

// The binder computes and commitFormFields publishes. A render shown to the
// user is a binder call, the at() calls its components make while rendering,
// then a commit (in React, a layout effect).
const form = (label = (key) => `«${key}»`) => {
  const state = sdk.formFieldState();
  const compute = sdk.fieldBinder(label, () => state);
  const bind = (root, current, change, issues, render) => {
    const bound = compute(root, current, change, issues);
    render?.(bound);
    sdk.commitFormFields(state);
    return bound;
  };
  return { state, compute, bind };
};

// The binder: labelled fields only, dotted names, translated label and hint,
// required exactly when Go rejects an empty value on the required path,
// writes into the latest values.
let values = { ...body, optional_text: "", supplied: "", optional_names: {} };
const update = (updater) => { values = updater(values); };
const { state, bind } = form();
const f = bind(op, values, update, [], (bound) => { bound.names.at("ms"); bound.names.at("en"); bound.codes.at(0); });
assert.deepEqual(Object.keys(f).sort(), ["account", "agreed", "choice", "codes", "consent", "count", "filter", "guarded", "links", "maybe", "min_text", "more_rows", "names", "nullable_text", "optional_names", "optional_text", "pin", "prefs", "price", "ratio", "refused", "rows", "secret", "share", "starts", "states", "supplied", "text", "transformed", "wallet"]);
assert.equal(sdk.FieldBinding, undefined, "the binding class is a type only: an entry exports no value for it");
assert.ok(Object.isFrozen(f.text));
assert.equal(f.text.name, "text");
assert.equal(f.text.label, "«fields.text»");
assert.equal(f.text.hint, "«help.text»");
const required = (binding) => binding.required;
assert.equal(required(f.text), true, "a non-blank rule");
assert.equal(required(f.min_text), true, "a positive length minimum");
assert.equal(required(f.secret), true, "the password rule");
assert.equal(required(f.choice), true, "a membership rule without an empty case");
assert.equal(required(f.supplied), true, "Required on an optional value requires it");
assert.equal(required(f.names.at("en")), true, "a locale Go requires");
assert.equal(required(f.optional_text), false, "a rule under Optional never requires");
assert.equal(required(f.nullable_text), false, "a nullable value is never required");
assert.equal(required(f.optional_names.at("en")), false, "an optional locale map requires no locale");
assert.equal(required(f.names.at("ms")), false);
assert.equal(required(f.count), false, "a number has no empty text");
assert.equal(required(f.transformed), false, "a transformed value publishes nothing");
assert.equal(required(f.guarded), false, "a conditional rule publishes nothing");
assert.equal(f.names.at("x.y").name, "names.x\\.y");
// A boolean is required, meaning it must be checked, exactly when Go demands
// true on the required path; no other rule requires it.
assert.equal(required(f.consent), true, "Required(Accepted()) on an optional boolean");
assert.equal(required(f.agreed), true, "Accepted on a required boolean");
assert.equal(required(f.refused), false, "OneOf(false) does not: the control would demand the one value Go rejects");
assert.equal(required(f.maybe), false, "Accepted on a value the form may leave out");
assert.deepEqual(plain(f.consent.checks), { required: true, limits: {} });
assert.deepEqual(plain(f.refused.checks), { required: false, limits: {} });
assert.equal(f.pin.required, true);
assert.deepEqual(plain(f.share.limits), { min: 0, max: 1, step: "any" });
assert.equal(f.text.row, false);

// A nested object binds as its labelled fields in the same call: names,
// labels, required and limits follow Go, and a write keeps its other fields.
assert.equal(f.account.username.name, "account.username");
assert.equal(f.account.username.label, "«fields.username»");
assert.equal(f.account.username.required, true);
assert.deepEqual(plain(f.account.username.limits), { nonEmpty: true, maxLength: 8 });
assert.equal(f.account.email.required, false, "a nullable nested value is never required");
assert.equal("internal" in f.account, false, "a nested field without a Go label is not bound");
assert.equal("value" in f.account, false, "a nested object is not a binding itself");
assert.ok(Object.isFrozen(f.account));
assert.equal(bind(op, values, update, []).account, f.account, "an unchanged nested object keeps its identity");
assert.equal(f.prefs.color.required, false, "an optional nested object requires none of its fields");
assert.deepEqual(plain(f.prefs.color.limits), { nonEmpty: true });
assert.throws(() => bind(field("account"), values.account, update, []), "a nested object is bound through its form's call, not as a root");
// An enum-keyed map binds an entry per declared key.
assert.equal(f.states.at("open").value, "yes");
assert.equal(f.states.at("shut").value, "", "an absent text entry shows as empty text");
assert.equal(f.states.at("open").label, "«fields.states»");
assert.throws(() => f.states.at("other"), "a key the enum does not declare");
assert.equal(f.names.at("ms").value, "", "an absent text entry shows as empty text");

assert.deepEqual([f.count.numeric, f.ratio.numeric, f.price.numeric, f.text.numeric], ["integer", "float", "decimal", undefined]);
assert.deepEqual([f.starts.temporal, f.text.temporal, f.count.temporal], ["date_time", undefined, undefined], "which time Go reads, so a picker never takes its kind from a prop");
// A range is compared exactly; text the comparison does not read is Go's to judge.
assert.equal(sdk.outOfRange(field("price").limits, "0.009999999999999999999"), "min");
assert.equal(sdk.outOfRange(field("price").limits, "0.01"), undefined);
assert.equal(sdk.outOfRange({ max: "10" }, "10.000000000000000001"), "max");
assert.equal(sdk.outOfRange(field("count").limits, 11), "max");
assert.equal(sdk.outOfRange(field("price").limits, "1,5"), undefined);
assert.equal(sdk.outOfRange(field("price").limits, "-"), undefined);

// While a field holds the value the form first bound it with, a control
// enforces only required; once it differs, every bound.
assert.deepEqual(plain(f.text.checks), { required: true, limits: {} });
assert.deepEqual(plain(f.count.checks), { required: false, limits: {} });
f.text.onChange("abcd");
f.names.at("ms").onChange("hai");
f.codes.at(0).onChange("cd");
assert.equal(values.text, "abcd");
assert.deepEqual(values.names, { en: "hello", ms: "hai" });
assert.deepEqual(values.codes, ["cd"]);
const g = bind(op, values, update, []);
assert.deepEqual(plain(g.text.checks), { required: true, limits: plain(field("text").limits) });
assert.deepEqual(plain(g.names.at("ms").checks.limits), { maxBytes: 12 }, "an edited locale enforces its bounds");
assert.deepEqual(plain(g.names.at("en").checks.limits), {}, "an untouched locale of the same map does not");
assert.deepEqual(plain(g.count.checks.limits), {});
assert.equal(g.count, f.count, "an unchanged field keeps its binding");
assert.notEqual(g.text, f.text);
// The form's state outlives a control: a remounted control or the next render
// binds through the same state and still sees the edit.
const remounted = sdk.fieldBinder((key) => `«${key}»`, () => state)(op, values, update, []);
sdk.commitFormFields(state);
assert.equal(remounted.text, g.text, "another binder on the same form state keeps edits and bindings");
assert.deepEqual(plain(form().bind(op, values, update, []).text.checks.limits), {}, "a new form instance starts unedited");
// A value changed back to the one first bound is not edited, and its bounds
// wait again.
g.text.onChange("abc");
assert.deepEqual(plain(bind(op, values, update, []).text.checks.limits), {}, "a restored field is not edited");
assert.deepEqual(plain(bind(op, { ...values, text: "abcd" }, update, []).text.checks.limits), plain(field("text").limits), "changed again, it is edited again");
g.text.onChange("abcd");
// Edited means changed since the form first bound the field, however it
// changed: by its own control, by another field's listener or by the update
// function.
{
  const titled = form();
  let draft = { ...body, text: "a" };
  const change = (next) => { draft = next(draft); };
  assert.deepEqual(plain(titled.bind(op, draft, change, []).text.checks.limits), {}, "as first bound");
  change((current) => ({ ...current, text: "too long a text" }));
  const normalized = titled.bind(op, draft, change, []);
  assert.deepEqual(plain(normalized.text.checks.limits), plain(field("text").limits), "changed by code, not by its control: edited");
  normalized.text.onChange("a");
  assert.deepEqual(plain(titled.bind(op, draft, change, []).text.checks.limits), {}, "changed back to its first-bound value by its control: not edited");
}

// Identity does not depend on the update function, and a kept binding writes
// through the committed one.
let writer = "";
const first = bind(op, values, (next) => { writer = "first"; update(next); }, []);
const second = bind(op, values, (next) => { writer = "second"; update(next); }, []);
assert.equal(second.count, first.count, "an inline update function keeps identity");
first.count.onChange(3);
assert.equal(writer, "second");
assert.equal(values.count, 3);

// A render that is computed and never committed changes nothing the form
// committed: a shown binding writes through the committed update function,
// the issues stay placed where they were, and a list the render shrank
// forgets no edit.
{
  const held = form();
  let draft = { ...body, rows: [{ name: "a", slots: [] }, { name: "b", slots: [] }] };
  const change = (next) => { draft = next(draft); };
  const issues = [{ path: "/body/rows/1/name", code: "row", message: "m" }];
  held.bind(op, draft, change, issues, (bound) => bound.rows.at(1).name).rows.at(1).name.onChange("bee");
  const shownRender = held.bind(op, draft, change, issues, (bound) => bound.rows.at(1).name);
  const row = shownRender.rows.at(1).name;
  assert.deepEqual(row.issues.map((issue) => issue.code), ["row"]);
  assert.deepEqual(plain(row.checks.limits), { nonEmpty: true, maxLength: 6 });
  // A transition renders with edits ignored, the first row removed and other
  // issues, then is dropped.
  const dropped = held.compute(op, { ...draft, rows: draft.rows.slice(1) }, () => {}, [{ path: "/body/text", code: "other", message: "m" }]);
  dropped.rows.at(0).name.onChange("lost");
  assert.equal(draft.rows[0].name, "lost", "even a dropped render's binding writes through the committed update function");
  change((current) => ({ ...current, rows: current.rows.map((each, at) => (at === 0 ? { ...each, name: "a" } : each)) }));
  shownRender.count.onChange(7);
  assert.equal(draft.count, 7, "a shown binding still writes through the committed update function");
  const next = held.bind(op, draft, change, issues, (bound) => bound.rows.at(1).name);
  assert.deepEqual(next.rows.at(1).name.issues.map((issue) => issue.code), ["row"], "the committed issues stay where they were placed");
  assert.deepEqual(plain(next.rows.at(1).name.checks.limits), { nonEmpty: true, maxLength: 6 }, "the dropped render's list shrink forgot no edit");
  assert.equal(next.rows.at(1).name, row, "and the shown binding keeps its identity");
}

// A binding first created after the form committed (in an event handler or a
// child's own re-render) still remembers its first-bound value for the next
// commit, and keeps its identity until the value changes.
{
  const lazy = form();
  let draft = { ...body, names: { en: "hello", ms: "old" } };
  const change = (next) => { draft = next(draft); };
  const bound = lazy.bind(op, draft, change, []);
  // Neither "ms" nor "rows.0.name" was bound before commitFormFields ran.
  const msFirst = bound.names.at("ms");
  assert.deepEqual(plain(msFirst.checks.limits), {}, "first bound after commit");
  assert.equal(bound.names.at("ms"), msFirst, "a second post-commit call keeps identity");
  msFirst.onChange("new");
  const afterEdit = lazy.bind(op, draft, change, [], (each) => each.names.at("ms"));
  assert.deepEqual(plain(afterEdit.names.at("ms").checks.limits), { maxBytes: 12 }, "edited against the value first bound after commit");
  afterEdit.names.at("ms").onChange("old");
  assert.deepEqual(plain(lazy.bind(op, draft, change, [], (each) => each.names.at("ms")).names.at("ms").checks.limits), {}, "restoring the post-commit first-bound value clears limits");
  // Even without a form render in between, commitFormFields publishes what a
  // post-commit binding learned.
  const rowFirst = afterEdit.rows.at(0).name;
  assert.deepEqual(plain(rowFirst.checks.limits), {});
  sdk.commitFormFields(lazy.state);
  change((current) => ({ ...current, rows: [{ ...current.rows[0], name: "changed" }] }));
  const afterCommit = lazy.bind(op, draft, change, [], (each) => each.rows.at(0).name);
  assert.deepEqual(plain(afterCommit.rows.at(0).name.checks.limits), { nonEmpty: true, maxLength: 6 }, "post-commit row baseline survived into the next commit");
}

// A render whose root binding throws leaves no uncommitted render attached to
// the form state: a later commitFormFields publishes nothing from the failed
// call.
{
  let fail = false;
  const guarded = form((key) => {
    if (fail && key === "fields.text") throw new Error("boom");
    return `«${key}»`;
  });
  let draft = { ...body, rows: [{ name: "a", slots: [] }, { name: "b", slots: [] }] };
  let lastWriter = "";
  const change1 = (next) => { lastWriter = "first"; draft = next(draft); };
  const change2 = (next) => { lastWriter = "second"; draft = next(draft); };
  const issues1 = [{ path: "/body/rows/1/name", code: "row", message: "m" }];
  guarded.bind(op, draft, change1, issues1, (bound) => bound.rows.at(1).name).rows.at(1).name.onChange("bee");
  const shown = guarded.bind(op, draft, change1, issues1, (bound) => bound.rows.at(1).name);
  // Invalid tuple root throws before attaching a render.
  assert.throws(() => guarded.compute([op, field("names")], { ...draft, rows: draft.rows.slice(1) }, change2, []));
  sdk.commitFormFields(guarded.state);
  // A throwing label partway through bindRoot also leaves no render attached.
  fail = true;
  assert.throws(() => guarded.compute(op, { ...draft, rows: draft.rows.slice(1) }, change2, [{ path: "/body/text", code: "other", message: "m" }]), /boom/);
  fail = false;
  sdk.commitFormFields(guarded.state);
  shown.count.onChange(9);
  assert.equal(lastWriter, "first", "a failed render did not replace the committed update function");
  assert.equal(draft.count, 9);
  const next = guarded.bind(op, draft, change1, issues1, (bound) => bound.rows.at(1).name);
  assert.deepEqual(next.rows.at(1).name.issues.map((issue) => issue.code), ["row"], "a failed render did not replace committed issues");
  assert.deepEqual(plain(next.rows.at(1).name.checks.limits), { nonEmpty: true, maxLength: 6 }, "a failed render did not forget committed edits");
}

// Two operations bound to one draft get separate bindings; a prepared
// operation's controls enforce nothing, but show Go's facts.
const prepared = sdk.operation("genericEcho");
assert.equal(prepared.preparation, true);
const p = bind(prepared, values, update, []);
const i = bind(op, values, update, []);
assert.notEqual(p.count, i.count);
assert.equal(p.text.required, true);
assert.deepEqual(plain(p.text.limits), plain(field("text").limits));
assert.deepEqual(plain(p.text.checks), { required: false, limits: {} }, "an edited field of a prepared request is still not enforced");
assert.deepEqual(plain(p.consent.checks), { required: false, limits: {} }, "nor a boolean Go requires to be true");

// A form that submits several operations binds them in one call, on its one
// state: separate bindings per operation, one record of first-bound values.
let reads = 0;
const shared = sdk.formFieldState();
const several = sdk.fieldBinder((key) => key, () => { reads++; return shared; });
const [x, y] = several([op, prepared], values, update, []);
sdk.commitFormFields(shared);
assert.equal(reads, 1, "the form's state is read once per call");
assert.notEqual(x.count, y.count);
x.text.onChange("abcde");
const [x2, y2] = several([op, prepared], values, update, []);
sdk.commitFormFields(shared);
assert.deepEqual(plain(x2.text.checks.limits), plain(field("text").limits), "the change counts for the operation it was made through");
assert.deepEqual(plain(y2.text.checks), { required: false, limits: {} }, "a prepared operation still enforces nothing");
assert.deepEqual(plain(several(op, values, update, []).text.checks.limits), plain(field("text").limits), "and for every binder of the same form");
assert.throws(() => several([op], values, update, []), "one operation is bound without brackets");
assert.throws(() => several([op, op], values, update, []), "an operation is listed once");
assert.throws(() => several([op, field("names")], values, update, []), "only operations are bound together");

// A create and an update whose bodies differ in shape: one call, the account
// nested in one and flat in the other.
const [creating, updating] = bind([op, sdk.operation("membersUpsert")], { ...values, username: "flat", email: "" }, update, []);
assert.equal(creating.account.username.name, "account.username");
assert.equal(updating.username.name, "username");
assert.equal(updating.username.value, "flat");

// Nested writes reach the latest values and keep the object's other fields;
// a write to an absent optional object creates it.
f.account.username.onChange("bob");
assert.deepEqual(values.account, { username: "bob", email: null, internal: "x" });
f.prefs.color.onChange("red");
assert.deepEqual(values.prefs, { color: "red" });
assert.deepEqual(plain(bind(op, values, update, []).account.username.checks.limits), { nonEmpty: true, maxLength: 8 }, "an edited nested field enforces its bounds");
assert.equal(bind(op, values, update, [{ path: "/body/account/username", code: "taken", message: "m" }]).account.username.issues[0].code, "taken");

// Rows: a form adds and removes entries through its own update; each binds
// with at(i) like a field of the element type and writes into the latest
// array at its position.
update((current) => ({ ...current, codes: [...current.codes, ""] }));
const rows = bind(op, values, update, [], (bound) => bound.codes.at(1));
assert.equal(rows.codes.at(1).value, "", "a new text row");
assert.equal(rows.codes.at(1).row, true, "a row of a list: the form removes it, a control never clears it");
assert.throws(() => rows.codes.at(2), "a row the list does not hold");
rows.codes.at(1).onChange("ef");
assert.deepEqual(values.codes, ["cd", "ef"]);
assert.deepEqual(plain(bind(op, values, update, []).codes.at(1).checks.limits), { maxLength: 4 }, "an edited row enforces its bounds");
update((current) => ({ ...current, codes: current.codes.filter((_, index) => index !== 0) }));
const shorter = bind(op, values, update, [], (bound) => bound.codes.at(0));
assert.equal(shorter.codes.at(0).value, "ef");
assert.deepEqual(plain(shorter.codes.at(0).checks.limits), {}, "a list whose length changed binds its rows anew");
shorter.codes.at(0).onChange("gh");
assert.deepEqual(plain(bind(op, values, update, []).codes.at(0).checks.limits), { maxLength: 4 }, "until a row's value changes again");

// Rows of fields, and a list inside each row.
const r = bind(op, values, update, []);
assert.equal(r.rows.at(0).name.name, "rows.0.name");
assert.equal(r.rows.at(0).name.label, "«fields.row_name»");
assert.equal(r.rows.at(0).name.required, true);
assert.equal(r.rows.at(0).slots.at(0).opens.name, "rows.0.slots.0.opens");
assert.deepEqual(plain(r.rows.at(0).slots.at(0).opens.limits), { minLength: 4 });
assert.equal("issues" in r.rows.at(0), false, "a row of fields is not a binding itself");
assert.throws(() => r.rows.at(1));
r.rows.at(0).slots.at(0).opens.onChange("1000");
assert.deepEqual(values.rows, [{ name: "a", slots: [{ opens: "1000" }] }]);

// Rows are addressed by position, as the server reports them: row i is what
// the list holds at index i, whatever code made the list.
const code = (issue) => issue.code;
update((current) => ({ ...current, rows: [...current.rows, { name: "b", slots: [] }], more_rows: [{ name: "x", slots: [] }] }));
bind(op, values, update, [], (bound) => bound.rows.at(1).name).rows.at(1).name.onChange("bee");
const reported = [{ path: "/body/rows/0/name", code: "first", message: "m" }, { path: "/body/rows/1/name", code: "second", message: "m" }, { path: "/body/more_rows/0/name", code: "other", message: "m" }];
const before = bind(op, values, update, reported, (bound) => bound.rows.at(1).name);
assert.deepEqual(before.rows.at(1).name.issues.map(code), ["second"]);
assert.deepEqual(before.rows.issues.map(code), ["first", "second"], "a list shows its rows' issues");
assert.deepEqual(plain(before.rows.at(1).name.checks.limits), { nonEmpty: true, maxLength: 6 });
assert.deepEqual(plain(before.rows.at(0).name.checks.limits), {});
// A row that code outside the binder replaces with a new object keeps its
// issue and its edit: nothing but its position names it.
update((current) => ({ ...current, rows: current.rows.map((each, at) => (at === 1 ? { ...each, slots: [{ opens: "0800" }] } : each)) }));
const replaced = bind(op, values, update, reported, (bound) => bound.rows.at(1).name);
assert.deepEqual(replaced.rows.at(1).name.issues.map(code), ["second"], "a row replaced by a new object keeps its issue");
assert.deepEqual(plain(replaced.rows.at(1).name.checks.limits), { nonEmpty: true, maxLength: 6 }, "and its edit");
// The updater may run twice, as React's strict mode runs it: a row write is a
// plain function of the values, so both runs agree.
const twice = (updater) => { updater(values); values = updater(values); };
bind(op, values, twice, reported).rows.at(1).name.onChange("bea");
assert.deepEqual(values.rows.map((each) => each.name), ["a", "bea"], "a row writes at its own position, once");
// After the list's length changes, no row shows the issues reported for its
// positions: they stay on the list's own binding and in the form's summary.
// Another list of the same rows keeps its issues on its rows.
update((current) => ({ ...current, rows: current.rows.slice(1) }));
const removed = bind(op, values, update, reported, (bound) => bound.rows.at(0).name);
assert.equal(removed.rows.at(0).name.value, "bea");
assert.equal(removed.rows.at(0).name.name, "rows.0.name");
assert.deepEqual(removed.rows.at(0).name.issues, [], "a position whose row may have moved shows no issue");
assert.deepEqual(removed.rows.issues.map(code), ["first", "second"], "the list's own binding still shows its rows' issues");
assert.deepEqual(removed.more_rows.at(0).name.issues.map(code), ["other"], "another list keeps its rows' issues");
assert.deepEqual(plain(removed.rows.at(0).name.checks.limits), {}, "a length change binds the list's rows anew");
// The list stays stale once its length is back: a row added after a removal
// does not show the removed row's issue.
update((current) => ({ ...current, rows: [...current.rows, { name: "c", slots: [] }] }));
const readded = bind(op, values, update, reported, (bound) => bound.rows.at(1).name);
assert.equal(readded.rows.at(1).name.value, "c");
assert.deepEqual(readded.rows.at(1).name.issues, [], "the same length again does not bring the old row issues back");
assert.deepEqual(readded.rows.at(0).name.issues, []);
assert.deepEqual(readded.rows.issues.map(code), ["first", "second"]);
// The next submit's issues are placed by position again.
const again = [{ path: "/body/rows/0/name", code: "again", message: "m" }];
assert.deepEqual(bind(op, values, update, again).rows.at(0).name.issues.map(code), ["again"]);
// A list below a stale list's rows is stale too.
const deep = [{ path: "/body/rows/0/slots/0/opens", code: "slot", message: "m" }];
const placed = bind(op, values, update, deep);
assert.deepEqual(placed.rows.at(0).slots.issues.map(code), ["slot"]);
assert.deepEqual(placed.rows.at(0).slots.at(0).opens.issues.map(code), ["slot"]);
update((current) => ({ ...current, rows: [{ name: "new", slots: [{ opens: "0700" }] }, ...current.rows] }));
const below = bind(op, values, update, deep);
assert.deepEqual(below.rows.at(0).slots.issues, [], "the rows of a stale list hide the issues of the lists they hold");
assert.deepEqual(below.rows.at(0).slots.at(0).opens.issues, []);
assert.deepEqual(below.rows.issues.map(code), ["slot"]);
update((current) => ({ ...current, rows: current.rows.slice(1) }));
// Equal rows are told apart by position alone.
update((current) => ({ ...current, codes: ["", ""] }));
const equal = bind(op, values, update, [{ path: "/body/codes/1", code: "row1", message: "m" }], (bound) => { bound.codes.at(0); bound.codes.at(1); });
assert.deepEqual(equal.codes.at(0).issues, []);
assert.deepEqual(equal.codes.at(1).issues.map(code), ["row1"]);
equal.codes.at(0).onChange("ab");
const afterEqual = bind(op, values, update, [{ path: "/body/codes/1", code: "row1", message: "m" }]);
assert.deepEqual(plain(afterEqual.codes.at(0).checks.limits), { maxLength: 4 });
assert.deepEqual(plain(afterEqual.codes.at(1).checks.limits), {}, "an edit of one equal row is not the other's");
// An issues array that is new on every render, of new issue objects, is
// placed anew each time: by position, without error.
for (const name of ["n1", "n2", "n3"]) {
  update((current) => ({ ...current, rows: [...current.rows, { name, slots: [] }] }));
  const each = bind(op, values, update, [{ path: "/body/rows/1/name", code: "fresh", message: "m" }]);
  assert.deepEqual(each.rows.at(1).name.issues.map(code), ["fresh"]);
  assert.deepEqual(each.rows.at(0).name.issues, []);
}
// An issue at an index the list does not reach, at the list itself or at a
// nested object itself is on no row or field: the list's binding shows the
// first two, and the form's summary shows them all.
const odd = bind(op, values, update, [{ path: "/body/rows/9/name", code: "beyond", message: "m" }, { path: "/body/rows", code: "list", message: "m" }, { path: "/body/account", code: "object", message: "m" }]);
assert.deepEqual(odd.rows.issues.map(code), ["beyond", "list"]);
for (let at = 0; at < values.rows.length; at++) assert.deepEqual(odd.rows.at(at).name.issues, []);
assert.deepEqual([odd.account.username.issues, odd.account.email.issues], [[], []]);
assert.deepEqual(bind(op, values, update, []).rows.at(1).name.issues, [], "issues follow the list the form passes");

// A nested object and a list of objects bind without a label of their own;
// a field below them still needs its Go label.
const unlabelled = bind(op, { ...values, wallet: { address: "0x1", note: "n" }, links: [{ role: "owner" }] }, update, []);
assert.equal(unlabelled.wallet.address.name, "wallet.address");
assert.equal(unlabelled.wallet.address.value, "0x1");
assert.equal("note" in unlabelled.wallet, false);
assert.equal(unlabelled.links.at(0).role.name, "links.0.role");
assert.equal(unlabelled.links.at(0).role.label, "«fields.role»");
assert.equal("value" in unlabelled.links, false, "a list without a label of its own binds by row only");
assert.ok(Object.isFrozen(unlabelled.links));
unlabelled.links.at(0).role.onChange("member");
assert.deepEqual(values.links, [{ role: "member" }]);
// A self-referencing object binds as far as its type does, and no further.
const nested = bind(op, { ...values, filter: { name: "a", not: { name: "b" } } }, update, []);
assert.equal(nested.filter.name.value, "a");
assert.deepEqual(Object.keys(nested.filter.not), [], "the binder stops where the type repeats");
// A list the values do not hold reads as empty rows; a write to its first row
// creates it.
let loose = { text: "x" };
const absent = form((key) => key).bind(op, loose, (next) => { loose = next(loose); }, []);
assert.equal(absent.codes.at(0).value, "");
assert.equal(absent.rows.at(2).name.value, undefined);
const unchanged = loose;
absent.codes.at(1).onChange("no");
assert.equal(loose, unchanged, "a later row of an absent list has nowhere to go: the values stay as they were");
absent.codes.at(0).onChange("zz");
assert.deepEqual(loose, { text: "x", codes: ["zz"] });

const h = bind(op, values, update, [{ path: "/body/names/ms", code: "x", message: "m" }]);
assert.equal(h.names.at("ms").issues[0].code, "x");
assert.equal(h.names.at("en").issues[0].code, "x", "an entry without issues shows its map's");
// An upload binds before a file is chosen, takes the chosen one and clears.
let upload = { title: "Profile" };
const uploads = form();
const u = uploads.bind(sdk.operation("uploadsProfile"), upload, (next) => { upload = next(upload); }, []);
assert.equal(u.document.value, undefined);
assert.equal(u.document.required, true, "a required file part");
assert.equal(u.document.name, "document");
assert.equal(u.document.row, false, "a single upload may be cleared");
const file = { data: new Blob(["x"]), filename: "a.txt" };
u.document.onChange(file);
assert.equal(upload.document, file);
uploads.bind(sdk.operation("uploadsProfile"), upload, (next) => { upload = next(upload); }, []).document.onChange(undefined);
assert.equal(upload.document, undefined);
// Repeated parts bind a row per upload or text value; a JSON part binds its
// labelled fields.
let gallery = { title: "Profile", photos: [file], "tags[]": ["a"], settings: { caption: "" } };
const g2 = form().bind(sdk.operation("uploadsProfile"), gallery, (next) => { gallery = next(gallery); }, []);
assert.equal(g2.photos.at(0).value, file);
assert.equal(g2.photos.at(0).name, "photos.0");
assert.equal(g2.photos.at(0).label, "«fields.photos»");
assert.equal(g2.photos.at(0).row, true, "a repeated upload is a row: the form removes it");
assert.deepEqual(plain(g2.photos.at(0).limits), plain(g2.photos.limits), "every upload carries the part's limits");
const replacement = { data: new Blob(["y"]), filename: "b.txt" };
g2.photos.at(0).onChange(replacement);
assert.deepEqual(gallery.photos, [replacement]);
g2["tags[]"].at(0).onChange("b");
assert.deepEqual(gallery["tags[]"], ["b"]);
assert.equal(g2.settings.caption.name, "settings.caption");
assert.equal(g2.settings.caption.label, "«fields.caption»");
g2.settings.caption.onChange("Hello");
assert.deepEqual(gallery.settings, { caption: "Hello" });
assert.deepEqual(gallery.photos, [replacement], "each write keeps the others");

// The descriptors of rows and map entries belong to the form that binds them:
// a form that visits many keys keeps nothing once it is gone.
{
  v8.setFlagsFromString("--expose-gc");
  const collect = vm.runInNewContext("gc");
  const settle = () => { for (let round = 0; round < 4; round++) collect(); return process.memoryUsage().heapUsed; };
  const visit = () => {
    const keys = form((key) => key);
    let many = { ...body, names: {} };
    const bound = keys.bind(op, many, (next) => { many = next(many); }, [], (each) => { for (let at = 0; at < 20000; at++) each.names.at(`key-${at}`); });
    return bound.names.at("key-0").name;
  };
  const baseline = settle();
  assert.equal(visit(), "names.key-0");
  const growth = settle() - baseline;
  assert.ok(growth < 4 * 1024 * 1024, `a closed form keeps its entry descriptors: ${growth} bytes retained`);
}

assert.throws(() => sdk.fieldBinder());
assert.throws(() => sdk.fieldBinder((key) => key));
assert.throws(() => sdk.fieldBinder((key) => key, () => ({}))(op, values, update, []), "a state not made by formFieldState");
assert.throws(() => sdk.commitFormFields({}), "a state not made by formFieldState");
