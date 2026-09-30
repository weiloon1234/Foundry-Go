import assert from "node:assert/strict";

function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; }

export async function checkForms(sdk, api, request) {
  const op = sdk.operation("itemsEcho");
  const original = { ...request, body: { ...request.body, tags: [...request.body.tags] } };
  const form = sdk.createForm(op, original);
  original.body.tags.push("caller mutation");
  assert.deepEqual(form.getSnapshot().values.body.tags, request.body.tags);
  assert.equal(form.getSnapshot(), form.getSnapshot());
  assert.equal(form.getSnapshot().dirty, false);
  assert.throws(() => { form.getSnapshot().values.body.large = "2"; }, TypeError);
  const amount = form.field(op.field("body", "amount"));
  const optional = form.field(op.field("body", "optional"));
  assert.equal(optional.getSnapshot().present, false);
  optional.set(null); assert.equal(optional.getSnapshot().value, null);
  optional.set(""); assert.equal(optional.getSnapshot().value, "");
  optional.unset(); assert.equal(optional.getSnapshot().present, false);
  assert.throws(() => optional.set(undefined), sdk.ContractError);
  amount.touch(); assert.equal(amount.getSnapshot().touched, true);
  amount.setText("999999999999999999999999999999.123456789");
  assert.equal(form.validate().issues[0].code, "form_unparsed");
  assert.equal(amount.parse(), true);
  assert.equal(amount.getSnapshot().value, "999999999999999999999999999999.123456789");
  amount.setText("2");
  assert.equal(amount.parse(() => { form.reset(); return "3"; }), false);
  assert.equal(amount.getSnapshot().value, request.body.amount);
  assert.equal(form.getSnapshot().issues.length, 0);
  const large = form.field(op.field("body", "large"));
  large.setText("9223372036854775808"); assert.equal(large.parse(), false);
  assert.equal(large.getSnapshot().value, request.body.large);
  assert.equal(large.getSnapshot().text, "9223372036854775808");
  // Built-in parsing is exact: no trimming, and the text "null" is not a value.
  for (const text of [" 42", "42\n", "null", "+1"]) {
    large.setText(text); assert.equal(large.parse(), false); assert.equal(large.getSnapshot().value, request.body.large);
  }
  large.setText("9223372036854775807"); assert.equal(large.parse(), true);
  assert.equal(large.getSnapshot().value, "9223372036854775807");
  assert.throws(() => large.set(123), sdk.ContractError);
  const tags = op.field("body", "tags");
  form.field(tags.at(0)).set("edited");
  assert.equal(form.getSnapshot().values.body.tags[0], "edited");
  assert.throws(() => form.field(tags.at(100)).set("sparse"), sdk.ContractError);
  assert.throws(() => form.field(tags.at(0)).unset(), sdk.ContractError);
  assert.throws(() => form.field(tags.element()), sdk.ContractError);
  assert.throws(() => form.field(sdk.schema("foundry.test/consumer/clientcontracts.Payload").field("amount")), sdk.ContractError);
  assert.throws(() => form.field(sdk.operation("formsSubmit").field("body", "name")), sdk.ContractError);
  form.field(tags.at(0)).setText("old index"); form.field(tags).set(["replacement"]);
  assert.equal(Object.keys(form.getSnapshot().text).length, 0);
  const key = op.field("body", "keys").at("9223372036854775807");
  form.field(key).set("max exact"); assert.equal(form.field(key).getSnapshot().value, "max exact");
  form.reset(); assert.equal(form.getSnapshot().dirty, false);
  assert.equal(form.getSnapshot().touched.length, 0);
  assert.equal(form.validate().complete, false); // uppercase remains server-only
  const sent = await form.submit(api);
  assert.equal(sent.status, "succeeded"); assert.equal(sent.value.large, request.body.large);
  form.field(op.field("body", "natural")).set("my");
  const server = await form.submit(api); assert.equal(server.status, "failed");
  assert.ok(server.error instanceof sdk.APIError);
  assert.equal(form.getSnapshot().issues[0].path, "/body/natural");
  assert.equal(form.field(op.field("body", "natural")).getSnapshot().issues[0].code, "foundry.uppercase");
  form.reset();

  let notifications = 0;
  const listener = () => { notifications++; };
  const stop = form.subscribe(listener), stop2 = form.subscribe(listener);
  optional.touch(); assert.equal(notifications, 2); stop();
  optional.set("changed"); assert.equal(notifications, 3); stop2();
  optional.set("again"); assert.equal(notifications, 3);
  const observerErrors = [];
  const guarded = sdk.createForm(op, request, { onListenerError: error => observerErrors.push(error) });
  guarded.subscribe(() => guarded.reset()); guarded.reset(); assert.equal(observerErrors.length, 1); guarded.dispose();
  const selectedErrors = [], replacedErrors = [], config = { onListenerError: error => selectedErrors.push(error) };
  const configured = sdk.createForm(op, request, config);
  config.onListenerError = error => replacedErrors.push(error);
  const configuredTask = configured.task(async () => null);
  configuredTask.subscribe(() => { throw new Error("observer failure"); });
  await configuredTask.run(); configured.dispose();
  assert.ok(selectedErrors.length > 0); assert.equal(replacedErrors.length, 0);

  const pending = deferred(); let signal, calls = 0;
  const wrapped = Object.create(api, { itemsEcho: { value: api.itemsEcho, writable: true } });
  for (const phase of ["idle", "submitting"]) {
    const early = sdk.createForm(op, request), abort = new AbortController(); let invoked = 0;
    const call = { signal: abort.signal };
    wrapped.itemsEcho = async () => { invoked++; return request.body; };
    early.subscribe(() => { if (early.getSnapshot().status === phase) { call.signal = new AbortController().signal; abort.abort(); } });
    const result = await early.submit(wrapped, call);
    assert.equal(result.status, "canceled"); assert.equal(result.outcome, "not_sent");
    assert.equal(invoked, 0); assert.equal(early.getSnapshot().pending, false); early.dispose();
  }
  // A signal-like object that is not an AbortSignal is refused before the guard.
  await assert.rejects(form.submit(wrapped, { signal: { aborted: false } }), sdk.ContractError);
  assert.equal(form.getSnapshot().pending, false);
  wrapped.itemsEcho = async (_, options) => { calls++; signal = options.signal; return pending.promise; };
  const old = form.submit(wrapped); assert.equal(form.getSnapshot().pending, true);
  await assert.rejects(form.submit(wrapped), sdk.ContractError);
  // Edits never abort a sent request: its actual outcome is still reported.
  optional.set("new input"); assert.equal(signal.aborted, false);
  assert.equal(form.getSnapshot().pending, true);
  await assert.rejects(form.submit(wrapped), sdk.ContractError);
  pending.resolve(request.body);
  const completed = await old;
  assert.equal(completed.status, "succeeded"); assert.equal(completed.changed, true); assert.equal(completed.value, request.body);
  assert.equal(form.getSnapshot().status, "idle"); assert.equal(form.getSnapshot().pending, false); assert.equal(calls, 1);
  assert.equal(optional.getSnapshot().value, "new input");
  // A failure after an edit is returned without replacing the newer draft's state.
  const rejected = deferred(); wrapped.itemsEcho = async () => rejected.promise;
  const failing = form.submit(wrapped); optional.set("newer input"); rejected.reject(new Error("server failure"));
  const failed = await failing;
  assert.equal(failed.status, "failed"); assert.equal(failed.changed, true); assert.equal(form.getSnapshot().issues.length, 0);
  assert.equal(form.getSnapshot().status, "idle");

  const aborted = deferred(), outer = new AbortController();
  wrapped.itemsEcho = async () => aborted.promise;
  const canceled = form.submit(wrapped, { signal: outer.signal }); outer.abort();
  assert.equal(form.getSnapshot().pending, true); aborted.reject(new Error("aborted"));
  // Cancellation after sending ends only this client's wait.
  const canceledResult = await canceled;
  assert.equal(canceledResult.status, "canceled"); assert.equal(canceledResult.outcome, "unknown"); assert.equal(form.getSnapshot().pending, false);
  const disposedResult = deferred(); wrapped.itemsEcho = async (_, options) => { signal = options.signal; return disposedResult.promise; };
  const disposing = form.submit(wrapped); form.dispose(); assert.equal(form.getSnapshot().pending, true); assert.equal(signal.aborted, true);
  // A transport that completes anyway reports what the server did.
  disposedResult.resolve(request.body);
  const afterDispose = await disposing; assert.equal(afterDispose.status, "succeeded"); assert.equal(afterDispose.changed, true);
  assert.equal(form.getSnapshot().pending, false); assert.throws(() => form.reset(), sdk.ContractError);

  const prepared = sdk.createForm(sdk.operation("formsSubmit"), { body: { name: "  Native form  ", "tags[]": [] } });
  assert.equal(prepared.validate().complete, false);
  assert.ok(prepared.validate().skipped.includes("request_preparation"));
  const preparedResult = await prepared.submit(api);
  assert.equal(preparedResult.status, "succeeded"); assert.equal(preparedResult.value.name, "Native form");
  prepared.field(sdk.operation("formsSubmit").field("body", "name")).set("denied");
  assert.equal((await prepared.submit(api)).status, "failed");
  assert.equal(prepared.getSnapshot().issues[0].path, ""); prepared.dispose();

  const file = new Blob(["owned form upload"], { type: "text/plain" });
  const upload = sdk.createForm(sdk.operation("uploadsProfile"), { body: { document: { data: file, filename: "form.txt" }, photos: [], "tags[]": [] } });
  assert.equal(upload.getSnapshot().values.body.document.data, file);
  assert.equal((await upload.submit(api)).status, "succeeded"); upload.dispose();

  const download = sdk.createForm(sdk.operation("download"), {}), stream = deferred(); let closed = 0;
  const downloads = Object.create(api, { download: { value: async () => stream.promise } });
  // A completed stream belongs to the caller even when the draft was reset meanwhile.
  const lateStream = download.submit(downloads); download.reset(); stream.resolve({ close: async () => { closed++; } });
  const streamed = await lateStream; assert.equal(streamed.status, "succeeded"); assert.equal(streamed.changed, true); assert.equal(closed, 0);
  await streamed.value.close(); assert.equal(closed, 1); download.dispose();

  const unionOp = sdk.operation("unionsEcho");
  const unionForm = sdk.createForm(unionOp, { body: { method: { kind: "card", token: "card-token", sequence: "7", labels: [] } } });
  const union = unionOp.field("body", "method"), tokenField = unionForm.field(union.variant("kind", "card").field("token"));
  tokenField.set("new token"); assert.equal(unionForm.getSnapshot().values.body.method.token, "new token");
  const card = unionForm.field(union.variant("kind", "card"));
  assert.equal(Object.hasOwn(card.getSnapshot().value, "kind"), false);
  assert.equal(Object.isFrozen(card.getSnapshot().value), true);
  card.set({ token: "whole payload", sequence: "8", labels: ["new"] });
  assert.equal(unionForm.getSnapshot().values.body.method.kind, "card");
  assert.equal(card.getSnapshot().value.token, "whole payload");
  assert.equal(tokenField.getSnapshot().value, "whole payload");
  card.set(card.getSnapshot().value); // Reading then writing the payload is valid.
  assert.equal((await unionForm.submit(api)).status, "succeeded");
  unionForm.field(union).set({ kind: "bank_transfer", reference: "bank" });
  assert.throws(() => tokenField.set("wrong variant"), sdk.ContractError);
  assert.throws(() => card.set({ token: "wrong payload", sequence: "8", labels: [] }), sdk.ContractError);
  assert.equal((await unionForm.submit(api)).status, "succeeded"); unionForm.dispose();

  const taskForm = sdk.createForm(op, request), runs = [], signals = [];
  const task = taskForm.task(async (_, signal) => { const work = deferred(); runs.push(work); signals.push(signal); return work.promise; });
  const a = task.run(), b = task.run(); assert.equal(signals[0].aborted, true);
  runs[1].resolve(["new option"]); assert.equal((await b).status, "succeeded");
  runs[0].resolve(["old option"]); assert.equal((await a).status, "stale");
  assert.deepEqual(task.getSnapshot().value, ["new option"]);
  const c = task.run(); taskForm.field(op.field("body", "optional")).set("edit");
  runs[2].resolve(["wrong input"]); assert.equal((await c).status, "stale"); assert.equal(task.getSnapshot().value, undefined);
  const d = task.run(); task.dispose(); assert.equal(task.getSnapshot().pending, 1);
  runs[3].resolve([]); await d; assert.equal(task.getSnapshot().pending, 0);
  let loaded = 0; const debounce = taskForm.task(async () => { loaded++; return "result"; }, { debounceMS: 25 });
  const before = debounce.run(); debounce.cancel(); assert.equal((await before).status, "canceled"); assert.equal(loaded, 0);
  assert.equal((await debounce.run()).status, "succeeded"); assert.equal(loaded, 1);
  debounce.dispose();
  const stuck = []; const bounded = taskForm.task(async () => { const work = deferred(); stuck.push(work); return work.promise; });
  const owners = [bounded.run(), bounded.run(), bounded.run(), bounded.run()];
  await assert.rejects(bounded.run(), sdk.ContractError); bounded.cancel();
  await assert.rejects(bounded.run(), sdk.ContractError);
  for (const work of stuck) work.resolve("done"); await Promise.all(owners);
  assert.equal(bounded.getSnapshot().pending, 0); bounded.dispose();
  // Runs replaced or invalidated while still debouncing hold no capacity, so
  // re-running several tasks from one input handler is never refused.
  let started = 0; const debounced = [0, 1, 2, 3].map(() => taskForm.task(async () => { started++; return "ok"; }, { debounceMS: 20 }));
  const first = debounced.map(item => item.run()), second = debounced.map(item => item.run());
  taskForm.field(op.field("body", "optional")).set("typed"); assert.equal(debounced[0].getSnapshot().pending, 0);
  const third = debounced.map(item => item.run());
  assert.deepEqual((await Promise.all(first)).map(result => result.status), ["canceled", "canceled", "canceled", "canceled"]);
  assert.deepEqual((await Promise.all(second)).map(result => result.status), ["canceled", "canceled", "canceled", "canceled"]);
  assert.deepEqual((await Promise.all(third)).map(result => result.status), ["succeeded", "succeeded", "succeeded", "succeeded"]);
  assert.equal(started, 4); for (const item of debounced) item.dispose();
  // An abort listener that starts a new run sees the edit that aborted it.
  let seen; const listening = taskForm.task(async (draft, signal) => {
    if (seen === undefined) signal.addEventListener("abort", () => { seen = null; listening.run().then(result => { seen = result.value; }); }, { once: true });
    return draft.body?.optional;
  });
  const hanging = listening.run(); taskForm.field(op.field("body", "optional")).set("latest");
  await hanging; await new Promise(resolve => setTimeout(resolve, 0)); assert.equal(seen, "latest");
  listening.dispose(); taskForm.dispose();

  const independent = sdk.createForm(op, request); assert.equal(independent.getSnapshot().dirty, false); independent.dispose();
  const cyclic = {}; cyclic.body = cyclic;
  assert.throws(() => sdk.createForm(op, cyclic), sdk.ContractError);
  const accessor = {}; Object.defineProperty(accessor, "body", { enumerable: true, get() { throw new Error("must not execute"); } });
  assert.throws(() => sdk.createForm(op, accessor), sdk.ContractError);
  assert.throws(() => sdk.createForm(op, request, { limits: { Bytes: 10, Depth: 10, Nodes: 100, Steps: 100, Issues: 10 } }), sdk.ContractError);
  console.log("PASS form state, exact parsing, presence, ownership, actual HTTP/server issues/multipart, cancellation, reported late outcomes and bounded async tasks");
}
