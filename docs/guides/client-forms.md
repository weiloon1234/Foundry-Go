# Forms bound to the generated SDK

<a id="agent-entry"></a>
## Agent entry

- Bind fields: [Form values](#recipe-form-values)
- Limits, labels and required: [Field facts](#recipe-field-facts)
- Preserve presence: [Omitted, null and empty](#recipe-form-presence)
- Submit: [Outcomes and issues](#recipe-form-outcomes)
- Public surface: [Reference](#recipe-form-reference)

## Overview

A form field is written once, as the generated field. The Go request
declaration owns its name, label, help text, required state and limits, and
the server owns its issues; the frontend keeps the values and draws the
controls. There is one way to bind a form: the generated `fieldBinder`. The SDK
has no form state controller and no UI adapter; a React form keeps its values
in component state and submits with its own action, and other frameworks do the
same with their own state.

<a id="recipe-form-values"></a>
## Bind a form's values

[Source and imports in the matching GitHub tree](../../tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts).

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts#form-values -->
```typescript
export function formValues(issues: readonly sdk.Issue[]) {
  type Body = sdk.Operations["itemsEcho"]["request"]["body"];
  let values: Required<Pick<sdk.FormValues<Body>, "amount">> = { amount: "999.99" };
  // A React state setter fits `update`; it receives a function of the values.
  const update: sdk.FormUpdate<typeof values> = (next) => { values = next(values); };
  // One state per form instance, kept while it is mounted (in React, a hook).
  const form = sdk.formFieldState();
  const fields = sdk.fieldBinder((key) => key, () => form);
  const amount = fields(sdk.operation("itemsEcho"), values, update, issues).amount;
  // Binding computes; the form publishes the render it shows (in React, a
  // layout effect), and its bindings then write through its update function.
  sdk.commitFormFields(form);
  amount.onChange("123456789012345678901234567890.123");
  return { name: amount.name, label: amount.label, limits: amount.limits, checks: amount.checks, issues: amount.issues, values };
}
```

`fieldBinder(label, form)` makes a binder. `label` translates a field's Go label
and help keys; `form` returns the state of the form instance being bound, made
by `formFieldState()` and kept while that form is mounted (in React, a hook
that holds it in state; the binder calls `form` once per call, so it may be a
hook). The state remembers the bindings it handed out, the length of each
list, the issues with the lists they point into, and the value each field held
when the form first bound it: a field is edited while its value differs from
that one, however it changed (its control, another field's listener, the
update function or code), so a field changed back is not edited.

Binding a form only computes: it changes nothing the form has committed.
`commitFormFields(form)` publishes what the latest call learned (its update
function, its issues and list lengths, the values it first bound) once the
render that made the call is the one shown; in React that is a layout effect
of the form component. A render React computes and discards (a transition that
suspends, a render it restarts) therefore changes nothing: a binding from the
shown render keeps writing through the shown render's update function until
the next commit, and a list the discarded render shortened forgets nothing.
A render whose root binding throws leaves no uncommitted render attached to
the form state. A binding first created after commit (for example in an event
handler or a child component's own re-render via `.at(...)`) still remembers
its first-bound value for the next commit. Before a form's first commit its
bindings write through the call that made them.

`fields(operation, values, update, issues)` returns the bindings of the fields
of the operation's request body that the form's values type holds and the
request binds: a field a control shows binds only with a Go label. A form calls
its binder once. A form
that submits several operations passes them together,
`const [created, updated] = fields([create, update], values, update, issues)`,
and receives one set of bindings per operation, in that order, all on the
form's one state: the values must fit every operation's request, and each
control binds through the operation the form will submit. A child or per-row
component receives the bindings it shows as props and never calls the binder
again for the same form. `update` receives a function
from the current values to the next; a React state setter fits. Values are the
control values: `FormValues<T>` holds each field as its non-null type (a nested
object as its own values, a list as its rows), and the form decides what the
request sends (see [presence](#recipe-form-presence)). A form may leave a key
out of its values until it has one, such as an upload before a file is chosen:
its binding's value is then `undefined`.

Bindings follow the request's structure as Go declares it:

- A nested object binds as its fields: `f.account.bank_name`. Each field a
  control shows needs a Go label; the object needs none of its own. The object
  itself is not a binding; its own issues stay in the form's summary. An
  optional or nullable nested object is held in the values like any other and
  the request builder sends, omits or nulls it. A nested field Go declares
  optional is optional in `FormValues`; declare it required in the form's own
  values type to bind it to a text control. A type that refers to itself binds
  as far as its generated type goes: where the type repeats, an object binds
  no fields.
- A list with a Go label binds whole (`f.permissions`, for a picker) and each
  row with `.at(i)`: a row of plain values binds like a field of the element
  type and shows the list's label (`f.tags.at(i)`), a row of objects as its
  fields (`f.items.at(i).name`), a list inside a row the same way
  (`f.days.at(i).intervals.at(j).opens`). A list of objects without a label of
  its own binds by row only (`f.links.at(i).role`). `.at(i)` exists only when
  the form's values type always holds the list; a list the values lack at run
  time reads as empty rows and a write to its first row creates it. `.at(i)`
  past the end of a list the values hold throws.
- Rows are addressed by position, as the server reports issues: row `i` is
  whatever the list holds at index `i`, so the form adds, removes, moves and
  replaces rows through its own `update` with nothing else to do. With the
  issues it is given, the binder remembers the length of every list they point
  into. Once a list's length has differed from that one, the list is stale
  until new issues arrive, even when its length is back (a row removed, then
  another added): no row of it, nor anything below a row, including the lists
  its rows hold, shows those issues; the list's own binding and the form's
  summary still do. A change of length also binds the list's rows anew: each
  row's first-bound value is the one it holds then, so its bounds wait until it
  changes. A reorder, or a removal and an insertion made between two renders
  of the form, can show a row's issue at the position the server reported
  until the next submit. Issues are placed against the values bound when they
  arrive, so a form keeps its fields disabled while a submit is pending.
- A map with a Go label binds whole and each entry with `.at(key)`, typed by
  the map's key (a locale, an enum case, a model ID or any text); its entries
  show the map's label, so a map without one does not bind. An entry without
  its own issues shows its map's.
- Multipart parts: a single upload or text part binds like a field (an upload
  as `FieldBinding<Upload | undefined>`, which the form may clear), a repeated
  part binds each upload or value with `.at(i)` as a list row (an upload as
  `FieldBinding<Upload>`, which no control clears: the form removes the row),
  and a JSON part binds as its fields (`f.metadata.caption`).

A JSON body that is itself a list or a union has no fields to bind. Path,
query and header parameters are not bound: a list filter, search box or pager
is an interface-only control with its own value.

A `FieldBinding<V>` carries `name`, `label`, `hint`, `required`, `limits`,
`checks`, `kind`, `numeric`, `temporal`, `row`, `value`, `onChange` and
`issues`. `temporal` tells which time Go reads from text (`date`, `time`,
`date_time` for an instant, `local_date_time` for a wall clock), so a picker
takes its kind from Go; `row` is true for a row of a list. Client entries
export it as a type only, and it has a private member, which TypeScript
compares nominally: an object literal, a spread of a binding, a copy with one
member replaced and the class value (`new FieldBinding()`,
`FieldBinding.prototype`) do not compile. A value typed `any`, such as the
result of `Object.create` or `JSON.parse`, is still assignable, as it is to
every TypeScript type. The name
is the field's path below its request location, dotted (`username`,
`title.en`, `account.bank_name`, `items.2.name`; a dot inside a key is
escaped), so it is unique in a form and keeps browser autofill heuristics.
Nothing reads submitted form data by name. `element()` descriptors are
templates and address no issue.

Within one form state a binding keeps its identity while its field's value,
label and issues and what it enforces are unchanged, whatever `update` function
the call passes (its `onChange` writes through the committed one), so memoised
controls skip renders. Each operation of a form has its own bindings; a
first-bound value belongs to the field, whichever operation bound it.

A field that is renamed or removed in Go fails to compile at `f.<name>`; a field
a control would show without a Go label has no binding (add `client:"label=…"`
to the Go field). A
binder's label function receives `FieldMessageKey`, the union of the label and
help keys that request fields of the entry declare; when one has no catalog
text, the application's `fieldLabel` line fails to compile and names the key.

## In a React form

An application makes one hook per client entry with its catalog-typed
translator, which holds the form's state and commits each shown render in a
layout effect. Each form calls it once per render, directly in the form
component's body, passing every operation it submits:

```tsx
// portals/admin/src/forms.ts: the one typed seam per portal
export const fieldLabel = (key: FieldMessageKey): string => t(key);
function useFormFieldState(): FormFieldState {
  const [state] = useState(formFieldState);
  useLayoutEffect(() => commitFormFields(state));
  return state;
}
export const useFields = fieldBinder(fieldLabel, useFormFieldState);

// a form
const create = operation("adminAccountsCreate");
const [draft, setDraft] = useState({ username: "", password: "" });
const f = useFields(create, draft, setDraft, feedback.issues);
<Input field={f.username} autoComplete="off" />
<Input field={f.password} autoComplete="new-password" />

// a form that creates or updates: one call, one state
const [created, updated] = useFields([create, update], draft, setDraft, feedback.issues);
const g = current ? updated : created;
<Input field={g.username} autoComplete="off" />
```

Build each request body with exactly its fields, as a fresh object literal
typed as the request (or, for a PATCH, an object of the request's type assigned
field by field). The request encoder rejects a key the request does not
declare. The compiler checks undeclared keys only in a fresh literal without a
spread: a spread, or a wider variable given to the request, compiles. A
type-aware convention test in the application (the baseline's
`web/build/form-conventions.test.ts`) rejects both at any depth (nested
objects, rows, map entries, and both branches of a conditional expression), a
second binder call in one component, a binder call outside a component's body
(named functions, anonymous default exports and expression-bodied arrow
components are recognised), and a form that validates or submits a body- or
multipart-carrying operation (`validateRequest("…")`, a generated API method
including destructured and bracket calls, an operation descriptor's `validate`
or `call`) it did not pass to its binder call; read, delete and action calls
without a request body pass. It cannot see an operation named only inside a
wrapper module, so a form keeps its validation call, or the generated API
call, in the component.

<a id="recipe-field-facts"></a>
## Limits, labels and required

`FieldDescriptor.limits` (and each binding's `limits`) is computed from the
operation's own rule tree, the same tree request validation walks. Nothing is
copied into a second form schema.

| Go rule | Counts | Limit | A control applies it |
|---|---|---|---|
| `MinLength`, `MaxLength`, `Length`, `LengthBetween`, `Password` | Unicode code points | `minLength`, `maxLength` | a custom check counting code points; never `minlength` or `maxlength`, which count UTF-16 units and would reject text Go accepts |
| `MaxBytes` | UTF-8 bytes | `maxBytes` | a custom check counting bytes |
| `Min`, `Max`, `DecimalMin`, `DecimalMax` | the value; fractional and wide bounds stay exact text | `min`, `max` | an integer or float: number input `min`/`max` within the range a browser compares exactly; a decimal: an exact comparison (`outOfRange`), text it cannot read left to Go; a slider: its ends (see below) |
| integer, number or decimal type, `DecimalMaxPlaces` | granularity | `step` (1 for an integer, `"any"` for a float, a decimal's places) | an integer or float number input `step`; a slider's step |
| multipart file part | bytes and types | `maxBytes`, `accept` | file input `accept` and a size check |
| `NonBlank`, `NonEmpty` and `Required` of text or a collection | emptiness | `nonEmpty` | with presence, `required` |

Rules publish nothing when the browser cannot apply them to the wire text: a
value whose Go form differs from its wire form (a named scalar, fixed array,
slice or map with its own codec, `json.Number`, `[]byte`) is marked
`transformed` in the rule metadata and skipped, as are conditional (`When`,
`Unless`) and cross-field rules and Go regular expressions. A rule that judges
a value its own type carries (`Required`, `RequiredNullable`, `Prohibited`, a
locale requirement, a pair comparison) takes that value's transformation, so
`Required` of a decoded type (including a custom-codec fixed array or map)
publishes no requirement either. Item counts are checked when the request is
validated and are not published to controls. `numeric` tells how Go reads a
number: an integer and a float bind to a number input (keeping intermediate
typed text such as an empty field, a lone minus sign or a partial decimal,
updating the bound value only when the text is a complete number, or empty for
a text-backed field); a decimal travels as exact text and binds to `<Input>`
as a text input with a decimal keyboard. On the wire, 64-bit integers and
decimals are strings (`FieldBinding<string>`) and bind to `<Input>` (which
renders `<input type="number">` for 64-bit integers and `<input type="text"
inputMode="decimal">` for decimals); 32-bit integers and binary floats are
JSON numbers (`FieldBinding<number>`) and bind to `<Slider>` (a range slider
when bounded, or a number input without a range).

A binding is `required` exactly when Go rejects an empty value: the field must
be present on every path to it (the wire requires the key and the value is not
null, or a `Required`, `Present` or `NotNull` rule that always runs requires
it), and a rule that always runs rejects empty: `NonBlank`, `NonEmpty` or
`Required` of text or a collection, a positive minimum length or item count,
the password rule, a membership rule without an empty case, an enum type, or a
required upload. A rule under `Optional`, `Nullable` or a pointer runs only on
a supplied value, so it never makes a field required. A boolean is never
empty: for a checkbox or switch "required" means "must be checked", and it is
required exactly when Go's `Accepted` rule demands true on the required path,
including under `Required(Accepted())`; no other rule makes it required
(`OneOf(false)` does not). A translated map
publishes which locales its rule requires (the `locales` parameter of its
`foundry.required` leaf, new manifest data), so `title.at("en")` is required
exactly when Go requires the default locale.

`checks` is what a control enforces before submit: `checks.required` blocks an
empty value (an unchecked box for a boolean), and `checks.limits` holds the
field's bounds once its value differs from the one the form first bound it
with (empty while it holds that value), so a stored value that predates a rule
never blocks saving other fields; the server judges what is sent. A request
with a preparation step (`http.PrepareEndpoint`) runs application code
(canonical slugs, trimming, sanitising) before Go validates it, so its
controls enforce nothing: they still show the required marker with
`aria-required`, the labels, hints, limits as facts and the issues Go reports.

A control never enforces before submit anything stricter than Go and never
changes a value the user did not change. The input type follows Go only where
it cannot be stricter: the password kind hides the text, and integer and float
fields are number inputs, with `min`, `max` and `step` from Go once their
value has changed (bounds a browser cannot compare exactly are left out).
Decimal, email and URL kinds stay text inputs; email and URL kinds set the
keyboard (`inputMode`) and autofill (`autoComplete`) hints, since a native
email or URL input refuses text Go may accept. A `password.Plaintext` field
must declare `client:"kind=password"` (generation fails without it).

A slider never invents a domain: its ends come from Go's bounds, and the
application may pass presentation `min` and `max` either to supply a missing
end or to narrow the track inside Go's published range (clamped to Go's range,
blocking nothing); with neither end it renders a number input that keeps typed
intermediate text (empty, lone minus sign, partial decimal) and updates the
bound value only on a complete number. It moves in Go's step (any value for a
float) or a coarser one the application gives. A stored value outside the
track or between its steps is shown and kept as it is until the user moves the
control. A fixed-length code (a PIN) draws Go's length (its published maximum)
and keeps the text as typed. An upload's chooser, the file name shown and the
bound value agree: the name is the bound upload's, and a cleared value clears
the chooser.

<a id="recipe-form-presence"></a>
## Omitted, null and empty

A PATCH sends only changed fields; `null` clears a nullable field; an empty
string is a supplied value. Controls edit the non-null value, and the form
builds the request from it:

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts#form-presence -->
```typescript
export function formPresence(values: { readonly optional: string }, stored: string | null): Pick<sdk.Operations["itemsEcho"]["request"]["body"], "optional"> {
  // The control edits text; the request decides presence: an empty value
  // clears the field (null), an unchanged one is omitted from the body.
  const next = values.optional.trim() ? values.optional : null;
  return next === stored ? {} : { optional: next };
}
```

Select the operation matching the user's edit instead of normalizing every
state to one value. [Wire values](client-contracts.md#recipe-wire-values) own
exact decimal, wide integer and temporal formats. Contact inputs use the backend
[phone parser and validation](phone.md) with one extension policy; do not
replace them with a browser regex.

<a id="recipe-form-outcomes"></a>
## Outcomes and issues

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts#form-outcomes -->
```typescript
type Submission =
  | { readonly status: "saved"; readonly value: sdk.Operations["itemsEcho"]["response"] }
  | { readonly status: "invalid"; readonly issues: readonly sdk.Issue[] }
  | { readonly status: "rejected"; readonly error: sdk.APIError }
  | { readonly status: "uncertain" };
export async function submitForm(api: sdk.API, request: sdk.Operations["itemsEcho"]["request"]): Promise<Submission> {
  try {
    return { status: "saved", value: await api.itemsEcho(request) };
  } catch (error) {
    // The client's own contract check failed: nothing was sent.
    if (error instanceof sdk.ContractError && !(error instanceof sdk.ResponseContractError)) return { status: "invalid", issues: error.issues };
    // A declared 4xx answer is definite; anything else may have been processed,
    // so reconcile before sending it again. No branch retries a mutation.
    if (error instanceof sdk.APIError && error.status >= 400 && error.status < 500 && error.status !== 408) return { status: "rejected", error };
    return { status: "uncertain" };
  }
}
```

A contract failure raised by the client means nothing was sent. A declared 4xx
answer is definite. Anything else, including a transport failure, a 5xx answer
or a response that breaks its contract, may have been processed: reconcile (for
example through an idempotency key) before sending again. No branch retries a
mutation. A form passes the decoded issues to its binder; `fieldIssues(descriptor,
issues)` returns the issues at a field or below it by the descriptor's own
escaped pointer, and `issueAt(descriptor, code, message)` addresses an issue the
client itself reports, such as a failed precondition about one input. Issues at
the form level, or at a field nothing renders, stay in the form's summary.

<a id="recipe-form-reference"></a>
## Reference

Every generated client entry exports the form-binding values and types below:

### Functions

- `formFieldState(): FormFieldState` — creates one empty binder state per
  mounted form instance.
- `fieldBinder(label, form): FieldBinder` — builds a binder whose `label`
  translates `FieldMessageKey` keys and whose `form` returns the mounted
  form's `FormFieldState`. Accepts one operation descriptor or a tuple of two
  or more distinct operation descriptors.
- `commitFormFields(form: FormFieldState): void` — publishes what the latest
  binder call on `form` computed (and any post-commit `.at(...)` bindings) as
  the form's committed state. Call once the render that bound the form is
  shown (in React, in a layout effect of the form component); a no-op when
  nothing is pending.
- `fieldIssues(descriptor, issues)` — returns the issues addressed to
  `descriptor` or below it, in order, matched by the descriptor's escaped
  JSON Pointer.
- `issueAt(descriptor, code, message)` — builds a frozen `Issue` addressed to
  `descriptor.path`.
- `outOfRange(limits, value)` — compares a number or exact decimal string
  against `limits.min` and `limits.max` without rounding, returning `"min"`,
  `"max"`, or `undefined`.

### Types

- `FieldBinding<V>` (exported as a type only, nominal via a private member):
  - `name: string` — location-relative dotted field path (`username`, `title.en`, `items.0.name`).
  - `label: string` — translated Go `client:"label=…"` text.
  - `hint: string | undefined` — translated Go `client:"help=…"` text, when declared.
  - `required: boolean` — true when Go rejects an empty value on the required path (or demands `true` via `Accepted` for a boolean).
  - `limits: FieldLimits` — Go's unconditional bounds (`nonEmpty`, `minLength`, `maxLength`, `maxBytes`, `min`, `max`, `step`, `accept`).
  - `checks: FieldChecks` — `{ required, limits }` enforced before submit (`limits` active only while `value` differs from its first-bound value; both empty for a prepared operation).
  - `kind: PresentationKind | undefined` — Go's presentation kind (`password`, `email`, `url`, `multiline`, etc.).
  - `numeric: "integer" | "float" | "decimal" | undefined` — how Go reads a numeric field.
  - `temporal: "date" | "time" | "date_time" | "local_date_time" | undefined` — which temporal format Go reads from text, so date/time pickers take their kind from Go.
  - `row: boolean` — true when the binding is a list row created with `.at(i)`, which a control never clears (the form removes the row instead).
  - `value: V` — the field's current form value.
  - `onChange: (value: V) => void` — updates the field through the form's committed update function.
  - `issues: readonly Issue[]` — issues at this field or below it (or its map's issues for a map entry without its own; hidden on rows of a stale list).
- `FieldLimits`, `FieldChecks`, `FieldPresentation`, `FieldMessageKey` — limits, pre-submit check state, typed presentation metadata, and the union of request-field label/help message keys.
- `FormFieldState`, `FieldBinder`, `FormUpdate<V>`, `FormValues<T>`, `BoundFields<T, V, L>`, `BoundField<T, V, L>`, `OperationBodyLabels` — per-form state, binder signature, state updater, form value projection, bound field groups, and the generated label tree.

## What applications change

See [form bindings](../compatibility.md#form-bindings-replace-the-form-controller)
in the compatibility notes.
