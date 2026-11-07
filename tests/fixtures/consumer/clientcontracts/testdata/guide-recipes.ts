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
