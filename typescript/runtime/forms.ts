import { APIError, ContractError, JSONNumber, ResponseContractError, checkAbort, defaultJSONLimits, limitsValid, numberPattern, object, reject, textBytes, unsentFailures, validUnicode } from "./runtime.js";
import type { CallOptions, ClientOptions, Issue, JSONLimits, Upload, ValidationReport, WireType } from "./runtime.js";
/** Editable request state. Missing values are allowed here, never asserted valid. */
export type FormDraft<T> = T extends Blob | JSONNumber | Upload ? T : T extends readonly (infer V)[] ? readonly FormDraft<V>[] : T extends object ? { readonly [K in keyof T]?: FormDraft<T[K]> } : T;
export type FormReadonly<T> = T extends Blob | JSONNumber ? T : T extends readonly (infer V)[] ? readonly FormReadonly<V>[] : T extends object ? { readonly [K in keyof T]: FormReadonly<T[K]> } : T;
export interface FormStore<S> {
  readonly getSnapshot: () => S;
  readonly subscribe: (listener: () => void) => () => void;
}
export type FormStatus = "idle" | "submitting" | "succeeded" | "failed" | "canceled" | "disposed";
export interface FormSnapshot<K extends keyof Operations> {
  readonly values: FormDraft<Operations[K]["request"]>;
  readonly text: Readonly<Record<string, string>>;
  readonly touched: readonly string[];
  readonly dirty: boolean;
  readonly issues: readonly Issue[];
  readonly validation: ValidationReport | undefined;
  readonly status: FormStatus;
  /** True until the actual invocation exits, even after cancel/reset/edit. */
  readonly pending: boolean;
  readonly error: unknown;
  readonly revision: number;
}
export interface FormFieldSnapshot<T> {
  readonly value: FormDraft<T> | undefined;
  readonly present: boolean;
  readonly text: string | undefined;
  readonly touched: boolean;
  readonly dirty: boolean;
  readonly issues: readonly Issue[];
}
export interface FormField<T> {
  readonly path: string;
  getSnapshot(): FormFieldSnapshot<T>;
  set(value: Exclude<T, undefined>): void;
  unset(): void;
  touch(): void;
  /** Does not change the parsed value. A pending draft blocks submission. */
  setText(text: string): void;
  /** Explicit parsing through the existing codec, or an application parser. */
  parse(parser?: (text: string) => Exclude<T, undefined>): boolean;
}
/** Owner paths rooted at operation K: field() or body(), then field/variant/at/element steps. */
type FormOwner<K> = readonly [K, string, string] | readonly [K, "body"] | readonly [FormOwner<K>, unknown] | readonly [FormOwner<K>, unknown, unknown];
/** Any field of operation K, whatever its value type. */
export interface FormDependency<K extends keyof Operations> { readonly [descriptorType]: (value: never, owner: never) => readonly [unknown, FormOwner<K>]; readonly path: string }
export interface FormOptions {
  readonly limits?: JSONLimits;
  readonly validation?: ClientOptions;
  /** Listeners observe state; they must not mutate it during notification. */
  readonly onListenerError?: (error: unknown) => void;
}
/**
 * A completed request always reports its actual result; `changed` means the draft
 * was edited, reset or disposed after it was sent. A failure's `outcome` says what
 * the client knows: `not_sent` (it never left the client), `error_response` (the
 * server answered with a declared error) or `unknown` (the transport failed or
 * the response broke its contract, so the server may have acted). Cancellation
 * only ends this client's wait; `unknown` again means it may have been processed.
 */
export type FormSubmission<T> = { readonly status: "succeeded"; readonly value: T; readonly changed: boolean } | { readonly status: "invalid"; readonly report: ValidationReport } | { readonly status: "failed"; readonly error: unknown; readonly changed: boolean; readonly outcome: "not_sent" | "error_response" | "unknown" } | { readonly status: "canceled"; readonly outcome: "not_sent" | "unknown" };
export interface FormTaskOptions<K extends keyof Operations = keyof Operations> {
  readonly debounceMS?: number;
  /**
   * The fields the callback reads; an element() template covers every entry.
   * Omitted, every edit invalidates the task. Declared, only a value write at,
   * above or below one of them does; unparsed text never does. Reset, cancel()
   * and disposal always do.
   */
  readonly dependsOn?: readonly FormDependency<K>[];
}
export interface FormTaskSnapshot<T> { readonly status: "idle" | "pending" | "succeeded" | "failed" | "canceled" | "disposed"; readonly value: FormReadonly<T> | undefined; readonly error: unknown; readonly pending: number }
/** `canceled`: a newer run, an edit, cancel() or disposal ended this run, whether or not its callback had started. */
export type FormTaskResult<T> = { readonly status: "succeeded"; readonly value: FormReadonly<T> } | { readonly status: "failed"; readonly error: unknown } | { readonly status: "canceled" };
export interface FormTask<T> extends FormStore<FormTaskSnapshot<T>> {
  run(): Promise<FormTaskResult<T>>;
  cancel(): void;
  dispose(): void;
}
export interface FormController<K extends keyof Operations> extends FormStore<FormSnapshot<K>> {
  readonly operation: OperationDescriptor<K>;
  field<T, O>(descriptor: FieldDescriptor<T, O> & ([O] extends [FormOwner<K>] ? unknown : never)): FormField<T>;
  validate(): ValidationReport;
  /** Exactly one invocation at a time. No implicit retries or replacement. Edits never abort it; cancel() and dispose() do. */
  submit(client: API, options?: CallOptions): Promise<FormSubmission<Operations[K]["response"]>>;
  reset(values?: FormDraft<Operations[K]["request"]>): void;
  cancel(): void;
  dispose(): void;
  /** Latest result wins; edits (or only those dependsOn declares), reset, cancel and disposal invalidate earlier work. */
  task<T>(load: (values: FormDraft<Operations[K]["request"]>, signal: AbortSignal) => Promise<T>, options?: FormTaskOptions<K>): FormTask<T>;
}
// Form bookkeeping has independent bounds; wire/validation limits remain owned
// by the operation. Async capacity counts callbacks until their actual exit.
const formPolicy = /* @__PURE__ */ Object.freeze({ listeners: 1024, tasks: 32, active: 4, debounceMS: 60000, dependencies: 64 });
// What an edit changed: everything (reset, cancel, disposal), only unparsed text,
// or the value at one concrete path.
type FormChange = undefined | "text" | readonly (string | number)[];
// A write at, above or below a dependency changes what it reads; a null
// (element template) segment matches every entry.
function formOverlaps(changed: readonly (string | number)[], dependency: readonly (string | number | null)[]): boolean {
  for (let i = 0; i < changed.length && i < dependency.length; i++) if (dependency[i] !== null && dependency[i] !== changed[i]) return false;
  return true;
}

function formCopy<T>(input: T, limits: JSONLimits): T {
  let nodes = 0, bytes = 0;
  const text = (value: string): void => { bytes += textBytes(value); if (!validUnicode(value) || bytes > limits.Bytes) reject("", "limit"); };
  const visit = (value: unknown, depth: number): unknown => {
    if (++nodes > limits.Nodes || depth > limits.Depth) reject("", "limit");
    if (value === undefined || value === null || typeof value === "boolean") return value;
    if (typeof value === "string") { text(value); return value; }
    if (typeof value === "number") { if (!Number.isFinite(value)) reject("", "type"); return value; }
    if (value instanceof JSONNumber) { text(value.text); return value; }
    // Blob bytes are immutable. Keep the caller's file without reading/copying it.
    if (typeof Blob !== "undefined" && value instanceof Blob) { bytes += value.size; if (bytes > limits.Bytes) reject("", "limit"); return value; }
    if (Array.isArray(value)) {
      if (value.length > limits.Nodes - nodes) reject("", "limit");
      const result: unknown[] = [];
      for (let i = 0; i < value.length; i++) {
        const property = Object.getOwnPropertyDescriptor(value, String(i));
        if (!property || !("value" in property)) reject("", "type");
        result.push(visit(property.value, depth + 1));
      }
      if (Reflect.ownKeys(value).length !== value.length + 1) reject("", "type");
      return Object.freeze(result);
    }
    const source = object(value), result: Record<string, unknown> = Object.create(null);
    for (const key of Object.keys(source)) { if (++nodes > limits.Nodes) reject("", "limit"); text(key); result[key] = visit(source[key], depth + 1); }
    return Object.freeze(result);
  };
  return visit(input, 0) as T;
}
function formEqual(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) return true;
  if (left instanceof JSONNumber && right instanceof JSONNumber) return left.text === right.text;
  if (left === null || right === null || typeof left !== "object" || typeof right !== "object") return false;
  if (typeof Blob !== "undefined" && (left instanceof Blob || right instanceof Blob)) return false;
  if (Array.isArray(left) !== Array.isArray(right)) return false;
  const a = Object.keys(left), b = Object.keys(right);
  return a.length === b.length && a.every(key => Object.hasOwn(right, key) && formEqual((left as Record<string, unknown>)[key], (right as Record<string, unknown>)[key]));
}
function formRead(input: unknown, segments: readonly (string | number | null)[]): { present: boolean; value: unknown } {
  let value = input;
  for (const key of segments) {
    if (key === null || value === null || typeof value !== "object" || !Object.hasOwn(value, key)) return { present: false, value: undefined };
    value = (value as Record<string | number, unknown>)[key];
  }
  return { present: true, value };
}
function formPayloadGuard(reference: DescriptorReference) {
  // Guards refer to ancestor values. An equal-length path is this descriptor's
  // selected payload; a child field has already moved below that boundary.
  return reference.guards.find(guard => guard.segments.length === reference.segments.length);
}
function formFieldRead(input: unknown, reference: DescriptorReference): { present: boolean; value: unknown } {
  const current = formRead(input, reference.segments), guard = formPayloadGuard(reference);
  if (!guard) return current;
  if (!current.present || current.value === null || typeof current.value !== "object" || (current.value as Record<string, unknown>)[guard.discriminator] !== guard.tag) return { present: false, value: undefined };
  // variant() describes the payload without its parent discriminator. Children
  // already belong to an immutable form snapshot; only this projection is new.
  const payload = Object.assign(Object.create(null), current.value);
  delete payload[guard.discriminator];
  return { present: true, value: Object.freeze(payload) };
}
function formWrite(input: unknown, segments: readonly (string | number | null)[], value: unknown, remove: boolean): unknown {
  const [key, ...tail] = segments;
  if (key === null || key === undefined) reject("", "form_field");
  const array = typeof key === "number";
  if (array ? !Array.isArray(input) || key >= input.length : input === null || typeof input !== "object" || Array.isArray(input)) reject("", "form_parent");
  if (array && remove && !tail.length) reject("", "form_array_remove");
  // Array insert/remove/reorder is an explicit parent set, avoiding ambiguous
  // index ownership. Nested parents and union variants must exist first.
  const result = array ? [...input as unknown[]] : Object.assign(Object.create(null), input);
  if (tail.length) result[key] = formWrite(result[key], tail, value, remove);
  else if (remove) delete result[key];
  else result[key] = value;
  return result;
}
function formScalarWire(type: WireType, text: string): string {
  if (type.kind === "quoted") return JSON.stringify(formScalarWire(resolvedDescriptor(type.element!).type, text));
  if (type.kind === "string") return JSON.stringify(text);
  // Numbers and booleans must be their exact JSON lexeme: the wire parser would
  // otherwise skip surrounding whitespace and accept the text "null".
  if (type.kind === "integer" || type.kind === "number") { if (!numberPattern.test(text)) reject("", "type"); return text; }
  if (type.kind === "boolean") { if (text !== "true" && text !== "false") reject("", "type"); return text; }
  reject("", "form_parser_required");
}
// Submission listens on the caller's signal and must always release its guard,
// so a JavaScript caller cannot pass an object that only resembles one.
function formSignal(signal: unknown): void {
  if (signal === undefined) return;
  const candidate = signal as Partial<AbortSignal> | null;
  if (candidate === null || typeof candidate !== "object" || typeof candidate.aborted !== "boolean" || typeof candidate.addEventListener !== "function" || typeof candidate.removeEventListener !== "function") reject("", "signal");
}
function formStore<S>(initial: S, onError?: (error: unknown) => void) {
  let snapshot = initial, notifying = false;
  const listeners = new Set<() => void>();
  return {
    getSnapshot: () => snapshot,
    subscribe(listener: () => void): () => void {
      if (typeof listener !== "function" || listeners.size >= formPolicy.listeners) reject("", "form_listener");
      // Independent subscriptions of the same function have independent cleanup.
      const wrapper = () => listener(); listeners.add(wrapper); return () => { listeners.delete(wrapper); };
    },
    mutable(): void { if (notifying) reject("", "form_notification"); },
    publish(next: S): void {
      snapshot = next; notifying = true;
      try { for (const listener of [...listeners]) if (listeners.has(listener)) { try { listener(); } catch (error) { try { onError?.(error); } catch { /* Observers never change outcomes. */ } } } }
      finally { notifying = false; }
    },
    clear(): void { listeners.clear(); },
  };
}

/** Own one form per screen/SSR request; credentials stay on its explicit client. */
export function createForm<K extends keyof Operations>(operation: OperationDescriptor<K>, initial: FormDraft<Operations[K]["request"]>, options: FormOptions = {}): FormController<K> {
  const registered = descriptorDocument().http.find(candidate => candidate.name === operation.name);
  if (!registered || operation.metadata !== registered || registered.body?.raw) reject("", "form_operation");
  const limits = { ...(options.limits ?? defaultJSONLimits) }; limitsValid(limits);
  const onListenerError = options.onListenerError;
  // Snapshot mutable option data. Explicit URL codec implementations remain
  // borrowed dependencies, just like the client and parser callbacks.
  const supplied = options.validation;
  const validationOptions = supplied && Object.freeze({ ...supplied,
    ...(supplied.headers ? { headers: formCopy(supplied.headers, limits) } : {}),
    ...(supplied.urlCodecs ? { urlCodecs: Object.freeze({ ...supplied.urlCodecs }) } : {}),
    ...(supplied.validationMessages ? { validationMessages: formCopy(supplied.validationMessages, limits) } : {}),
  });
  type Draft = FormDraft<Operations[K]["request"]>;
  let baseline = formCopy(initial, limits), values = baseline, revision = 0, disposed = false;
  object(values);
  let text: Readonly<Record<string, string>> = Object.freeze(Object.create(null));
  let touched: readonly string[] = Object.freeze([]), issues: readonly Issue[] = Object.freeze([]), validation: ValidationReport | undefined;
  let status: FormStatus = "idle", error: unknown, submission: AbortController | undefined, taskNotifying = false;
  const tasks = new Set<{ invalidate(change: FormChange): void; dispose(): void }>();
  let activeTasks = 0;
  const snapshot = (): FormSnapshot<K> => Object.freeze({ values, text, touched, dirty: !formEqual(values, baseline) || Object.keys(text).length > 0, issues, validation, status, pending: submission !== undefined, error, revision });
  const store = formStore(snapshot(), onListenerError);
  const live = (): void => { store.mutable(); if (taskNotifying) reject("", "form_notification"); if (disposed) reject("", "form_disposed"); };
  const emit = (): void => store.publish(snapshot());
  // Callers store their new state first: task abort listeners run synchronously
  // and must observe current values with the new revision. Edits never abort a
  // submission; only cancel() and dispose() do.
  const invalidate = (change?: FormChange): void => {
    revision++; issues = Object.freeze([]); validation = undefined; error = undefined; status = "idle";
    for (const task of [...tasks]) task.invalidate(change);
  };
  const checked = (descriptor: object): DescriptorReference => {
    const reference = descriptorReferences.get(descriptor);
    if (!reference || reference.operation !== operation.name || reference.segments.includes(null)) reject("", "form_field_owner");
    if (reference.segments.length > limits.Depth) reject("", "limit");
    for (const guard of reference.guards) {
      const parent = formRead(values, guard.segments).value;
      if (parent === null || typeof parent !== "object" || (parent as Record<string, unknown>)[guard.discriminator] !== guard.tag) reject("", "form_variant");
    }
    return reference;
  };
  const field = <T, O>(descriptor: FieldDescriptor<T, O>): FormField<T> => {
    live(); checked(descriptor);
    const path = descriptor.path;
    const clearText = (): void => {
      const next = Object.assign(Object.create(null), text);
      for (const key of Object.keys(next)) if (key === path || key.startsWith(path + "/")) delete next[key];
      text = Object.freeze(next);
    };
    const write = (value: unknown, remove: boolean): void => {
      live(); const reference = checked(descriptor);
      // Draft state may be incomplete. set() still checks a supplied field's
      // codec; validation owns all operation-level and cross-field rules.
      if (!remove && reference.id && !descriptor.repeated) contracts().encode(reference.id, value, limits);
      const guard = formPayloadGuard(reference);
      if (!remove && guard) value = Object.assign(Object.create(null), value, { [guard.discriminator]: guard.tag });
      const next = formCopy(formWrite(values, reference.segments, value, remove), limits) as Draft;
      values = next; clearText(); touched = Object.freeze(touched.filter(key => !key.startsWith(path + "/"))); invalidate(reference.segments as readonly (string | number)[]); emit();
    };
    return Object.freeze({
      path,
      getSnapshot(): FormFieldSnapshot<T> {
        const reference = checked(descriptor), current = formFieldRead(values, reference), original = formFieldRead(baseline, reference);
        return Object.freeze({ value: current.value as FormDraft<T> | undefined, present: current.present, text: text[path], touched: touched.includes(path), dirty: current.present !== original.present || !formEqual(current.value, original.value) || Object.hasOwn(text, path), issues: Object.freeze(issues.filter(issue => issue.path === path || issue.path.startsWith(path + "/"))) });
      },
      set(value: Exclude<T, undefined>): void { if (value === undefined) reject(path, "form_unset_required"); write(value, false); },
      unset(): void { write(undefined, true); },
      touch(): void { live(); checked(descriptor); if (!touched.includes(path)) { touched = formCopy([...touched, path], limits); emit(); } },
      setText(value: string): void {
        live(); checked(descriptor); if (typeof value !== "string") reject(path, "type");
        text = formCopy({ ...text, [path]: value }, limits); invalidate("text"); emit();
      },
      parse(parser?: (text: string) => Exclude<T, undefined>): boolean {
        live(); const reference = checked(descriptor); if (!Object.hasOwn(text, path)) return true;
        const version = revision;
        try {
          if (!parser && (!reference.id || descriptor.repeated)) reject("", "form_parser_required");
          const value = parser ? parser(text[path]!) : contracts().decode(reference.id!, formScalarWire(resolvedDescriptor(reference.id!).type, text[path]!), limits);
          if (disposed || revision !== version) return false;
          if (value === undefined) reject("", "form_unset_required");
          write(value, false); return true;
        } catch (caught) {
          if (disposed || revision !== version) return false;
          const found = caught instanceof ContractError ? caught.issues.map(issue => ({ ...issue, path: path + issue.path })) : [{ path, code: "form_parse" }];
          issues = formCopy([...issues.filter(issue => issue.path !== path && !issue.path.startsWith(path + "/")), ...found].slice(0, limits.Issues), limits); validation = undefined; emit(); return false;
        }
      },
    });
  };
  const validate = (): ValidationReport => {
    live();
    const version = revision;
    let report: ValidationReport;
    if (Object.keys(text).length) report = { issues: Object.keys(text).slice(0, limits.Issues).map(path => ({ path, code: "form_unparsed" })), complete: false, skipped: ["form_draft"] };
    else try { report = operation.validate(values as Operations[K]["request"], validationOptions); }
    catch (caught) { if (!(caught instanceof ContractError)) throw caught; report = { issues: caught.issues, complete: false, skipped: ["request_contract"] }; }
    if (disposed || revision !== version) reject("", "form_changed");
    validation = formCopy(report, limits); issues = validation.issues; error = undefined; emit(); return validation;
  };
  const cancel = (): void => { live(); invalidate(); status = "canceled"; submission?.abort(); emit(); };
  const controller: FormController<K> = {
    operation, getSnapshot: store.getSnapshot,
    subscribe(listener) { live(); return store.subscribe(listener); },
    field, validate,
    async submit(client, call = {}) {
      live(); if (submission) reject("", "form_busy");
      const signal = call.signal; formSignal(signal); checkAbort(signal);
      const report = validate(); if (report.issues.length) return { status: "invalid", report };
      const owner = new AbortController(); submission = owner;
      const attemptRevision = revision, request = values;
      const aborted = (): void => owner.abort(signal?.reason);
      // Edits, reset and disposal after sending do not abort the request; its
      // actual outcome is still returned, marked as changed.
      const changed = (): boolean => disposed || revision !== attemptRevision;
      let sent = false;
      try {
        signal?.addEventListener("abort", aborted, { once: true });
        // Validation callbacks/listeners may abort before this listener existed.
        if (signal?.aborted) aborted();
        status = "submitting"; error = undefined; emit();
        checkAbort(owner.signal);
        sent = true;
        const result = await operation.call(client, request as Operations[K]["request"], { ...call, signal: owner.signal });
        // Even after cancel/edit/dispose a completed request is reported; a file
        // or event-stream response is then owned by the caller like any success.
        const late = changed();
        if (!late) status = "succeeded";
        return { status: "succeeded", value: result, changed: late };
      } catch (caught) {
        if (owner.signal.aborted) {
          if (!changed()) status = "canceled";
          return { status: "canceled", outcome: sent ? "unknown" : "not_sent" };
        }
        const late = changed();
        // Only a contract failure the SDK raised before calling its transport
        // never left the client; a declared API error is the server's answer;
        // anything else, including a response that broke its contract or a
        // ContractError thrown by application code, leaves the outcome unknown.
        const outcome = caught instanceof APIError ? "error_response" : caught instanceof ContractError && unsentFailures.has(caught) ? "not_sent" : "unknown";
        // Server issues describe the request that was sent; a changed draft keeps
        // its own, newer state and the caller receives the failure.
        if (!late) {
          status = "failed"; error = caught;
          // Response issues point into the response, not at request fields.
          if (caught instanceof ResponseContractError) issues = Object.freeze([{ path: "", code: "form_response" }]);
          else if (caught instanceof ContractError) issues = formCopy(caught.issues, limits);
          else if (caught instanceof APIError && caught.response !== undefined) {
            // The SDK already decoded the shared error envelope. Validate wrapped
            // clients too before accepting its field pointers into form state.
            try {
              const envelope = contracts().decode(runtimeDocument().error_type, contracts().encode(runtimeDocument().error_type, caught.response, limits), limits) as { issues?: readonly Issue[] };
              issues = formCopy(envelope.issues?.length ? envelope.issues : [{ path: "", code: caught.code }], limits);
            } catch { issues = Object.freeze([{ path: "", code: "form_request" }]); }
          } else issues = Object.freeze([{ path: "", code: "form_request" }]);
        }
        return { status: "failed", error: caught, changed: late, outcome };
      } finally {
        // Release the guard first; nothing after it can keep the form busy.
        submission = undefined; signal?.removeEventListener("abort", aborted);
        if (status === "submitting") status = "canceled"; emit();
      }
    },
    reset(next = baseline) {
      live(); const owned = formCopy(next, limits); object(owned);
      baseline = owned; values = owned; text = Object.freeze(Object.create(null)); touched = Object.freeze([]); invalidate(); emit();
    },
    cancel,
    dispose() {
      if (disposed) return; live(); invalidate(); disposed = true; status = "disposed"; submission?.abort();
      for (const task of [...tasks]) task.dispose(); emit(); store.clear();
    },
    task<T>(load: (values: Draft, signal: AbortSignal) => Promise<T>, taskOptions: FormTaskOptions<K> = {}): FormTask<T> {
      live(); if (typeof load !== "function" || tasks.size >= formPolicy.tasks) reject("", "form_task_limit");
      const debounce = taskOptions.debounceMS ?? 0;
      if (!Number.isSafeInteger(debounce) || debounce < 0 || debounce > formPolicy.debounceMS) reject("", "form_debounce");
      // Snapshot the declared paths once. An empty list would silently ignore
      // every edit; omit dependsOn to depend on the whole draft.
      const declared: unknown = taskOptions.dependsOn;
      let dependencies: readonly (readonly (string | number | null)[])[] | undefined;
      if (declared !== undefined) {
        if (!Array.isArray(declared) || declared.length === 0 || declared.length > formPolicy.dependencies) reject("", "form_task_dependencies");
        const paths: (readonly (string | number | null)[])[] = [];
        for (const descriptor of [...declared] as unknown[]) {
          const reference = descriptor !== null && typeof descriptor === "object" ? descriptorReferences.get(descriptor) : undefined;
          if (!reference || reference.operation !== operation.name) reject("", "form_field_owner");
          if (reference.segments.length > limits.Depth) reject("", "limit");
          paths.push(Object.freeze([...reference.segments]));
        }
        dependencies = Object.freeze(paths);
      }
      const affected = (change: FormChange): boolean => !dependencies || change === undefined || (change !== "text" && dependencies.some(path => formOverlaps(change, path)));
      interface TaskRun { readonly abort: AbortController; started: boolean; released: boolean }
      let sequence = 0, dead = false, current: TaskRun | undefined, pending = 0;
      let taskStatus: FormTaskSnapshot<T>["status"] = "idle", taskValue: FormReadonly<T> | undefined, taskError: unknown;
      const taskSnapshot = (): FormTaskSnapshot<T> => Object.freeze({ status: taskStatus, value: taskValue, error: taskError, pending });
      const taskStore = formStore(taskSnapshot(), onListenerError);
      const taskEmit = (): void => { taskNotifying = true; try { taskStore.publish(taskSnapshot()); } finally { taskNotifying = false; } };
      const taskLive = (): void => { live(); taskStore.mutable(); if (dead) reject("", "form_task_disposed"); };
      // A run holds capacity until its callback actually exits. A run stopped
      // before its callback started (still debouncing) has nothing left to wait
      // for, so it releases capacity at once.
      const release = (run: TaskRun): void => { if (!run.released) { run.released = true; pending--; activeTasks--; } };
      // Callers finish their state before aborting: abort listeners run
      // synchronously and may start a new run.
      const stop = (): TaskRun | undefined => {
        sequence++; const run = current; current = undefined;
        if (run && !run.started) release(run);
        return run;
      };
      const invalidateTask = (): void => { const run = stop(); taskStatus = "canceled"; taskValue = undefined; taskError = undefined; run?.abort.abort(); taskEmit(); };
      const owner = { invalidate(change: FormChange): void { if (affected(change)) invalidateTask(); }, dispose(): void { if (dead) return; invalidateTask(); dead = true; taskStatus = "disposed"; taskEmit(); taskStore.clear(); tasks.delete(owner); } };
      tasks.add(owner);
      return Object.freeze({
        getSnapshot: taskStore.getSnapshot,
        subscribe(listener: () => void) { taskLive(); return taskStore.subscribe(listener); },
        cancel() { taskLive(); invalidateTask(); },
        dispose() { if (dead) return; live(); taskStore.mutable(); owner.dispose(); },
        async run(): Promise<FormTaskResult<T>> {
          taskLive();
          // Replacing this task's own not-yet-started run frees its capacity.
          if (activeTasks - (current && !current.started ? 1 : 0) >= formPolicy.active) reject("", "form_task_busy");
          const previous = stop(), run: TaskRun = { abort: new AbortController(), started: false, released: false };
          const token = ++sequence, request = values, abort = run.abort;
          // Every path that supersedes a run also aborts it, so one check covers
          // a newer run, an affecting edit, cancel() and disposal at any stage.
          const ended = (): boolean => abort.signal.aborted || dead || disposed || token !== sequence;
          current = run; pending++; activeTasks++; taskStatus = "pending"; taskValue = undefined; taskError = undefined;
          previous?.abort.abort(); taskEmit();
          try {
            if (debounce) await new Promise<void>(resolve => {
              const end = (): void => { clearTimeout(timer); abort.signal.removeEventListener("abort", end); resolve(); };
              const timer = setTimeout(end, debounce); abort.signal.addEventListener("abort", end, { once: true });
              if (abort.signal.aborted) end();
            });
            if (ended()) return { status: "canceled" };
            run.started = true;
            const result = await load(request, abort.signal);
            if (ended()) return { status: "canceled" };
            taskValue = formCopy(result, limits) as FormReadonly<T>; taskStatus = "succeeded"; return { status: "succeeded", value: taskValue };
          } catch (caught) {
            if (ended()) return { status: "canceled" };
            taskError = caught; taskStatus = "failed"; return { status: "failed", error: caught };
          } finally { release(run); if (current === run) current = undefined; taskEmit(); }
        },
      });
    },
  };
  return Object.freeze(controller);
}
