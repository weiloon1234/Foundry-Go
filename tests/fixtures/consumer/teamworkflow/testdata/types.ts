import * as sdk from "./contracts_foundry.gen.js";
declare const client: sdk.API;
type Reply = sdk.Operations["workflowPatch"]["response"];
declare const reply: Reply;
export function typedWorkflow(): void {
 const path = { team: "1", project: "shared" };
 void client.workflowPatch({ path, body: {} });
 void client.workflowPatch({ path, body: { title: null, budget: "0" } });
 void client.workflowPatch({ path, body: { title: "", budget: "42" } });
 // @ts-expect-error Explicit undefined cannot masquerade as omitted PATCH state.
 void client.workflowPatch({ path, body: { title: undefined } });
 // @ts-expect-error Wide numeric contracts remain exact strings.
 void client.workflowPatch({ path, body: { budget: 0 } });
 if (reply.data.kind === "updated") {
  const title: string | null = reply.data.title;
  const budget: string = reply.data.budget;
  // @ts-expect-error The queued payload is unavailable in the updated branch.
  const queued = reply.data.project_id;
  void [title,budget,queued];
 } else {
  const name: string = reply.data.name;
  // @ts-expect-error The project payload is unavailable in the queued branch.
  const title = reply.data.title;
  void [name,title];
 }
 const key = sdk.idempotencyKey("typed-workflow-request-0001");
 void client.workflowSubmit({ path, body: { name: "Example" }, idempotencyKey: key });
 // @ts-expect-error Durable submission has a required typed key.
 void client.workflowSubmit({ path, body: { name: "Example" } });
 void client.workflowCatalogue({ query: { team: "1", page: "1", per_page: "2" } });
 void client.workflowRulesForm({ body: { name: "null" } });
 // @ts-expect-error Form text has no implicit explicit-null state.
 void client.workflowRulesForm({ body: { name: null } });
 const failure: sdk.Operations["workflowSubmit"]["errorCode"] = "workflow_rejected";
 void failure;
}
type CaptureDetail = sdk.Operations["developerWebhookCapturesShow"]["response"];
declare const capture: CaptureDetail;
export function typedCaptures(): void {
 void client.developerWebhookCapturesList({ query: { from: "2026-11-11T00:00:00Z", mode: "full", path_prefix: "/wh/", per_page: "30" } });
 // @ts-expect-error The capture list always has a time bound.
 void client.developerWebhookCapturesList({ query: { mode: "full" } });
 // @ts-expect-error Body mode is a closed enum.
 void client.developerWebhookCapturesList({ query: { from: "2026-11-11T00:00:00Z", mode: "raw" } });
 void client.developerWebhookCapturesShow({ path: { request: "0192f3a4-5b6c-7d8e-9f01-23456789abcd" } });
 const state: sdk.CaptureBodyStateReceived = capture.request.body_state;
 const omission: sdk.Nullable_CaptureBodyOmissionReceived = capture.request.body_omission;
 // @ts-expect-error A missing status is null, never zero.
 const status: number = capture.request.status;
 void [state === "omitted" && omission, status, capture.body, capture.headers?.[0]?.values];
}
