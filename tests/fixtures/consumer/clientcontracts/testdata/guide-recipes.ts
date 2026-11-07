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
export function formValues() {
  const operation = sdk.operation("itemsEcho");
  const form = sdk.createForm(operation, { body: {}, path: { key: "1" } });
  const amount = form.field(operation.field("body", "amount"));
  amount.set("123456789012345678901234567890.123");
  amount.setText("999.99");
  amount.parse();
  const report = form.validate();
  form.dispose();
  return report;
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
export function formPresence(form: sdk.FormController<"itemsEcho">) {
  const field = form.field(form.operation.field("body", "optional"));
  field.unset();
  const omitted = field.getSnapshot();
  field.set(null);
  const cleared = field.getSnapshot();
  field.set("");
  const empty = field.getSnapshot();
  return { omitted, cleared, empty };
}
// doc-example:end form-presence

// doc-example:begin form-outcomes
export async function submitForm(form: sdk.FormController<"itemsEcho">, api: sdk.API) {
  const result = await form.submit(api);
  switch (result.status) {
    case "succeeded":
      return { result, issues: form.getSnapshot().issues };
    case "invalid":
      return { result, issues: result.report.issues };
    case "failed":
      if (result.error instanceof sdk.ResponseContractError) {
        return { result, reconcile: true };
      }
      return {
        result,
        reconcile: result.outcome === "unknown",
        issues: form.getSnapshot().issues,
      };
    case "canceled":
      return { result, reconcile: result.outcome === "unknown" };
    default: {
      const exhaustive: never = result;
      return exhaustive;
    }
  }
}
// doc-example:end form-outcomes
