import * as sdk from "./contracts_foundry.gen.js";
import { createPortalSession, type HTTPTransport } from "./contracts_browser_foundry.gen.js";

// doc-example:begin wire-values
export function wireValues(body: sdk.Operations["itemsEcho"]["request"]["body"]) {
  const request: sdk.Operations["itemsEcho"]["request"] = {
    path: { key: "9223372036854775807" },
    body: { ...body, large: "9223372036854775807", amount: "1234567890.12", optional: null },
  };
  return request;
}
// doc-example:end wire-values

// doc-example:begin locale-fields
export function localeFields() {
  const titles: sdk.LocaleMap<string> = { en: "Hello", ms: "Helo" };
  const operation = sdk.operation("itemsEcho");
  const title = operation.field("body", "titles").at("en");
  const tag = operation.field("body", "tags").element();
  return { titles, title, tag };
}
// doc-example:end locale-fields

// doc-example:begin portal-lifecycle
export async function portalLifecycle(transport: HTTPTransport, baseURL: string, login: string, password: string) {
  const portal = createPortalSession(transport, { clientOptions: { baseURL } });
  try {
    await portal.restoreSession();
    const profile = await portal.signInWithPassword({ login_id: login, password });
    await portal.signOut();
    return profile;
  } finally {
    portal.session.dispose();
  }
}
// doc-example:end portal-lifecycle

// doc-example:begin form-values
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
// doc-example:end form-values

// doc-example:begin received-values
export function receivedValues(item: sdk.Operations["itemsEcho"]["response"]) {
  const state = item.state instanceof sdk.UnknownEnumValue
    ? item.state.value
    : item.state;
  const newerLocale: string | undefined = item.titles?.["fr"];
  return { state, newerLocale };
}
// doc-example:end received-values

// doc-example:begin enum-labels
export function enumLabels(state: sdk.Status | sdk.UnknownEnumValue, messages: sdk.ValidationMessages) {
  const choices = sdk.Status.cases;
  const label = sdk.enumLabel(sdk.Status.labels, state, key => sdk.formatMessage(messages, key));
  return { choices, label };
}
// doc-example:end enum-labels

// doc-example:begin schema-handles
export function schemaHandles(input: string) {
  const decoded = sdk.decodeContract(sdk.schemas.Clientcontracts_Payload, input);
  const checked = sdk.contractValue(sdk.schemas.Clientcontracts_Payload, decoded);
  const encoded = sdk.encodeContract(sdk.schemas.Clientcontracts_Payload, checked);
  const descriptor = sdk.schema(sdk.schemas.Clientcontracts_Payload);
  return { decoded, encoded, descriptor };
}
// doc-example:end schema-handles

// doc-example:begin field-descriptors
export async function fieldDescriptors(api: sdk.API, request: sdk.Operations["itemsEcho"]["request"]) {
  const operation = sdk.operation("itemsEcho");
  const amount = operation.field("body", "amount");
  const value: sdk.FieldValue<typeof amount> = request.body.amount;
  const report = operation.validate(request);
  const response = await operation.call(api, request);
  return {
    value,
    kind: amount.presentation.kind,
    required: amount.required,
    nullable: amount.nullable,
    report,
    response,
  };
}
// doc-example:end field-descriptors

// doc-example:begin issue-addresses
export function issueAddresses(issues: readonly sdk.Issue[]) {
  const tag = sdk.operation("itemsEcho").field("body", "tags").at(0);
  const label = sdk.operation("unionsEcho").field("body", "method")
    .variant("kind", "card").field("labels").at(0);
  const escaped = sdk.schema(sdk.schemas.ExplicitLabels).at("a/b~c");
  return {
    tag: issues.filter(issue => issue.path === tag.path),
    label: issues.filter(issue => issue.path === label.path),
    escapedPath: escaped.path,
    all: issues,
  };
}
// doc-example:end issue-addresses

// doc-example:begin public-environment
export function publicEnvironment(rawEnvironment: Record<string, string | undefined>) {
  const problems = sdk.findPublicEnvProblems(rawEnvironment, "VITE_");
  if (problems.length > 0) throw new Error(problems.join("\n"));
  const settings = sdk.decodePublicEnv(rawEnvironment);
  const dates = new Intl.DateTimeFormat(settings.formatLocale ?? settings.defaultLocale, {
    timeZone: settings.timeZone,
  });
  return { settings, dates };
}
// doc-example:end public-environment

// doc-example:begin localized-client
export function localizedClient(transport: sdk.HTTPTransport, messages: sdk.ValidationMessages) {
  const locale: sdk.Locale | undefined = sdk.catalogLocales?.default;
  const supported: readonly sdk.Locale[] = sdk.catalogLocales?.supported ?? [];
  const api = sdk.createClient(transport, {
    headers: { "Accept-Language": messages.locale },
    validationMessages: messages,
  });
  return { api, locale, supported };
}
// doc-example:end localized-client

// doc-example:begin form-presence
export function formPresence(values: { readonly optional: string }, stored: string | null): Pick<sdk.Operations["itemsEcho"]["request"]["body"], "optional"> {
  // The control edits text; the request decides presence: an empty value
  // clears the field (null), an unchanged one is omitted from the body.
  const next = values.optional.trim() ? values.optional : null;
  return next === stored ? {} : { optional: next };
}
// doc-example:end form-presence

// doc-example:begin form-outcomes
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
// doc-example:end form-outcomes

// doc-example:begin request-headers
export async function requestHeaders(api: sdk.API) {
  await api.headersInspect({
    query: { q: "active" },
    headers: { "X-App-Version": "2.4.0", "X-Sequence": "9223372036854775807" },
  });
}
// doc-example:end request-headers

// doc-example:begin validation-error-code
export function isValidationFailure(error: unknown): boolean {
  return error instanceof sdk.APIError && error.code === sdk.ErrorCode.ValidationFailed;
}
// doc-example:end validation-error-code

// doc-example:begin request-lifecycle
export function startRequest(
  api: sdk.API,
  request: sdk.Operations["itemsEcho"]["request"],
  receive: (value: sdk.Operations["itemsEcho"]["response"]) => void,
  report: (error: unknown) => void,
): () => void {
  const controller = new AbortController();
  void api.itemsEcho(request, { signal: controller.signal }).then(receive, (error: unknown) => {
    if (!controller.signal.aborted) report(error);
  });
  return () => controller.abort();
}
// doc-example:end request-lifecycle

// doc-example:begin form-state-provider
export function bindFormOperations(issues: readonly sdk.Issue[]) {
  type Draft = Required<Pick<sdk.FormValues<sdk.Operations["itemsEcho"]["request"]["body"]>, "amount">>;
  const label = (key: sdk.FieldMessageKey): string => key;
  let values: Draft = { amount: "0" };
  const update: sdk.FormUpdate<Draft> = (next) => { values = next(values); };

  const singleState = sdk.formFieldState();
  const singleFields = sdk.fieldBinder(label, () => singleState);
  const single = singleFields(sdk.operation("itemsEcho"), values, update, issues);
  sdk.commitFormFields(singleState);

  const combinedState = sdk.formFieldState();
  const combinedFields = sdk.fieldBinder(label, () => combinedState);
  const [items, generic] = combinedFields(
    [sdk.operation("itemsEcho"), sdk.operation("genericEcho")], values, update, issues,
  );
  sdk.commitFormFields(combinedState);
  return { single, items, generic };
}
// doc-example:end form-state-provider

// doc-example:begin submission-key
export function submissionKey(): sdk.IdempotencyKey {
  return sdk.idempotencyKey(crypto.randomUUID());
}
// doc-example:end submission-key
