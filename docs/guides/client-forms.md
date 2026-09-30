# Framework-neutral forms and optional frontend adapters

The generated SDK provides `createForm`; its
[acceptance evidence](../evidence/forms-startup-20260930.json) records the verified
source and consumer paths. The core remains plain ES2022 TypeScript/JavaScript with
no React, Vue or state-library imports. [Typed descriptors](client-descriptors.md)
and the existing codec/validation/call pipeline remain the contract owners.

## Ownership and examples

```ts
import { createForm, operation } from "./generated/contracts_foundry.gen.js";

const endpoint = operation("formsSubmit");
const form = createForm(endpoint, { body: { name: "", "tags[]": [] } });
const name = form.field(endpoint.field("body", "name"));
const stop = form.subscribe(() => render(form.getSnapshot()));

name.setText("  Ada  "); // Keeps input text separately from the request value.
name.touch();
if (name.parse()) {
  const result = await form.submit(client);
  if (result.status === "succeeded") showResult(result.value);
}
stop();
form.dispose();
```

The executable [consumer](../../tests/fixtures/consumer/clientcontracts/testdata/forms.mjs)
uses actual registered endpoints, HTTP, server issues and multipart files. These
operation names belong to that consumer, not to every generated application.
Applications choose controls, labels, layout and styles; the controller neither
renders a UI nor owns credentials. Create a controller per screen/request.

`FormDraft` allows incomplete request values without pretending they are valid
DTOs. Snapshots are owned, bounded and immutable. `field(descriptor)` preserves
its operation and value type; a reusable schema descriptor or another operation's
field is rejected. Root request containers (`body`, `query`, `path`) and nested
parents must exist before editing their children. `reset(newDraft)` establishes
new initial values, including any explicit idempotency key or signed URL.

Fields expose `set`, `unset`, `setText`, `parse`, `touch` and `getSnapshot`.
Unset, null, zero, false and an empty string remain distinct. Dirty state compares
against the reset baseline; pending text is dirty even if its parsed value has
not changed. Set replaces that field and clears its pending descendant text.
Changes invalidate previous validation/server issues and async results.

## Parsing and nested fields

Parsing is explicit. Until `parse()` succeeds, text does not replace the last
parsed value, and validation/submission reports `form_unparsed`. Built-in scalar
parsing delegates to the existing codec, including exact decimal and wide-integer
strings, quoted scalars and declared enums. It does not trim, round money, select
a timezone or treat an empty string as omission/null. A custom parser may return
the field's exact type; its result still passes the codec. Arbitrary parser
exception text is not turned into a user-visible message.

Use `.at(index)` for an existing array item and `.at(key)` for a map entry.
`.element()` is a descriptor template, so it cannot bind editable state. Set the
parent collection to insert/remove/reorder items; sparse indices and implicit
array deletion are rejected. For unions, select `.variant(discriminator, tag)`
and then its field. The selected variant must still match the current value.
Binding the selected payload itself exposes only its payload properties; setting
that payload preserves the parent discriminator. Set the parent union to switch
variants. Password fields remain dedicated request
inputs; no controller state is inserted into public metadata.

Use typed `Upload`/`Blob` values for file controls. Blob bytes remain immutable
and are retained without reading or copying their contents. File content/MIME
approval stays server-owned. Raw streaming request bodies are not form drafts;
use direct SDK calls for them. Form snapshots accept plain data, immutable exact
numbers and blobs; class instances, accessors, sparse arrays and cycles are
rejected or hit the existing depth bound. Defaults use the SDK's JSON limits;
`limits` can explicitly select a larger bounded draft budget, including blob sizes.
It never raises the operation's actual wire limits.

## Validation, errors and submission

`validate()` calls the existing operation validator and returns its full
`issues`, `complete`, `skipped` report. Contract errors become issues; preparation,
custom server checks and portable-rule gaps stay incomplete. Empty issues do not
mean server approval. Incompleteness alone does not prevent a server request.
Pass `validation` options for the same translated messages/custom URL codecs
used by the client; no second rule interpreter is introduced.

The controller maps the SDK's decoded server error envelope using its JSON
Pointers. Field snapshots include matching descendant issues; the full snapshot
retains all issues, including form-level failures and currently unmounted fields.
`error` retains the actual failure for application handling; do not render its
arbitrary internal fields or send them to logs automatically.

A form admits exactly one submission at a time. Additional calls reject with
`form_busy`; there are no mutation retries or implicit replacement. Edits,
reset/cancel/disposal abort and invalidate prior work. A late completion cannot
replace current state. `pending` stays true until the actual invocation exits,
even if its transport ignores cancellation. Stale stream responses are closed;
a successful file/event-stream response is owned by the caller and must be
consumed/closed. Cancellation does not prove the server rolled back an operation.

## Async options and checks

```ts
const suggestions = form.task(async (draft, signal) => {
  return loadOptions(draft.body?.name ?? "", signal);
}, { debounceMS: 150 });

name.setText(input.value);
if (name.parse()) await suggestions.run();
```

Each task has its own immutable store (`getSnapshot`, `subscribe`) and latest-run
sequence. Form edits invalidate old work. Debounce is explicit and defaults to
zero. Results are copied as bounded plain data; this interface is for option/check
data, not streamed response handles. Use the result to display suggestions or
advisory checks; it does not replace server validation or mark the request valid.

A form permits 32 task objects and four concurrently outstanding callbacks.
Cancellation/disposal keeps a callback's capacity until it actually exits. A
replacement is refused when capacity is full. Each store caps subscriptions at
1,024; unsubscribe/dispose releases them. Listeners only observe state; recursive
mutation during notifications is rejected. `onListenerError` can inspect listener
failures without allowing them to change a request's outcome.

## Optional React and Vue modules

Generate optional adapters with `typescript.Options{React: true, Vue: true}` or
`contracts:export --dir client/generated --react --vue`. The standalone
`foundry contracts` command accepts the same flags. Their default filenames are
`contracts_react_foundry.gen.ts` and `contracts_vue_foundry.gen.ts`; the prefix
option updates their core import. Both use the existing atomic artifact publisher
and freshness checks. Removing a flag removes that owned adapter on regeneration.

The React module exports `useForm(store, getServerSnapshot?)`, backed by
[`useSyncExternalStore`](https://react.dev/reference/react/useSyncExternalStore).
The Vue module exports `useForm(store)`, returning a readonly shallow ref and
releasing its subscription with the active
[Vue scope](https://vuejs.org/api/reactivity-advanced.html#onscopedispose).
Both accept forms and tasks, preserve their types, and borrow their controllers.
Unmounting does not dispose another component's shared form. The screen/request
owner calls `dispose`; Vue calls must run in setup/an active effect scope.

UI dependencies are optional peers of the consuming application/package. Importing
the core SDK never imports an adapter. Keep adapter exports on separate subpaths
such as `./react` and `./vue`; do not re-export them from the package's core entry.
Development-only acceptance passed actual React/Vue rendering, cleanup and SSR,
including React hydration and Vue ref-shaped snapshots. See the
[evidence](../evidence/forms-startup-20260930.json). SSR requires separate controllers
and clients per request. React hydration must receive the same initial snapshot;
never serialize credentials or password drafts into a page. Dispose request-owned
controllers in a finally block after server rendering.

## Starter adoption and B08

The framework owns the reusable controller, parsing, subscription and cancellation
logic. The starter adopts a published matching runtime/tool revision, regenerates
Go/manifest v6/OpenAPI/SDK together, and adds its own domain form examples and
optional package subpaths. The starter must not copy this runtime. Deployment,
publication and component styling remain application decisions. The
[starter handoff](forms-starter-handoff-20260930.md) records its current adopted
descriptor pin and concrete next steps.

This work does not explain or close B08, the historical Linux unclaimed-job
failure. Database startup diagnostics distinguish boot from worker execution on
future failures. The starter already retains its unchanged eight-second test,
worker logs and failing stack. Continue normal verification and diagnose actual
recurrences; extra passing repetition campaigns are not closure evidence.
