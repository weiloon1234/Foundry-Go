import * as sdk from "./contracts_foundry.gen.js";

type Payload = sdk.Operations["itemsEcho"]["request"]["body"];
declare const payload: Payload;
declare const api: sdk.API;
declare const realtime: sdk.Realtime;
type GenericUser = sdk.Operations["genericEcho"]["response"];
type GenericProject = sdk.Operations["genericProject"]["response"];
declare const genericUser: GenericUser;
declare const genericProject: GenericProject;

type Payment = sdk.Operations["unionsEcho"]["request"]["body"]["method"];
declare const payment: Payment;
type Echoed = sdk.Operations["itemsEcho"]["response"];
declare const echoed: Echoed;
type Upserted = sdk.Operations["membersUpsert"]["response"];
declare const upserted: Upserted;
type Continued = sdk.Operations["sessionContinue"]["response"];
declare const continued: Continued;
type MemberEvents = sdk.Operations["membersEvents"]["response"];
declare const memberEvents: MemberEvents;
export function typedConsumer(): void {
  const upsertStatus: 200 | 201 = upserted.status;
  const upsertDisplay: string = upserted.body.display;
  // @ts-expect-error Only declared success statuses can be received.
  const undeclaredStatus: 204 = upserted.status;
  const location: string = continued.location;
  const redirectStatus: 303 = continued.status;
  void api.filesRaw({ body: { data: new Blob(["raw"]), mediaType: "text/plain" } });
  void api.filesRaw({ body: { data: new Uint8Array([1]), mediaType: "application/octet-stream" } });
  void api.filesRaw({ body: { data: new ReadableStream<Uint8Array>(), mediaType: "application/octet-stream" } });
  // @ts-expect-error Several declared media types require an explicit choice.
  void api.filesRaw({ body: { data: new Blob(["raw"]) } });
  // @ts-expect-error Raw bodies accept only declared media types.
  void api.filesRaw({ body: { data: new Blob(["raw"]), mediaType: "image/png" } });
  // @ts-expect-error Raw data is binary, not text.
  void api.filesRaw({ body: { data: "text", mediaType: "text/plain" } });
  void (async () => {
    for await (const event of memberEvents) {
      const display: string = event.data.display;
      const name: string = event.name;
      const id: string | undefined = event.id;
      // @ts-expect-error Event data keeps its declared type.
      const wrong: number = event.data.display;
      void [display, name, id, wrong];
    }
  })();
  void [upsertStatus, upsertDisplay, undeclaredStatus, location, redirectStatus];
  type ExplicitLabels = sdk.ContractTypes["foundry.test/consumer/clientcontracts.ExplicitLabels"];
  const labels: ExplicitLabels = { "": "empty", "01": "text", ["__proto__"]: "ordinary" };
  const absentLabels: ExplicitLabels = null;
  // @ts-expect-error An unconstrained map key does not make its value dynamic.
  const invalidLabels: ExplicitLabels = { label: 1 };
  void [labels, absentLabels, invalidLabels];
  void api.formsSubmit({ body: { name: "Jane", "tags[]": ["one"] }, query: { name: "separate" } });
  // @ts-expect-error Form required fields cannot be supplied by the URL query.
  void api.formsSubmit({ body: {}, query: { name: "Jane" } });
  // @ts-expect-error Form repetition is an explicit scalar list.
  void api.formsSubmit({ body: { name: "Jane", "tags[]": "one" } });
  // @ts-expect-error Form text has no implicit null representation.
  void api.formsSubmit({ body: { name: "Jane", title: null } });
  switch (payment.kind) {
    case "card": {
      const token: string = payment.token;
      const sequence: string = payment.sequence;
      // @ts-expect-error Discrimination removes the unrelated payload.
      const reference = payment.reference;
      void [token, sequence, reference]; break;
    }
    case "bank_transfer": { const reference: string = payment.reference; void reference; break; }
    case "wrapped": { const token: string = payment.data.token; void token; break; }
    default: { const exhaustive: never = payment; void exhaustive; }
  }
  void api.unionsEcho({ body: { method: payment, more: [payment], maybe: null } });
  // @ts-expect-error An undeclared variant cannot enter the contract.
  const futurePayment: Payment = { kind: "future", token: "test", sequence: "1" };
  // @ts-expect-error Mixed payload fields are rejected.
  const mixedPayment: Payment = { kind: "card", token: "test", sequence: "1", reference: "mixed" };
  // @ts-expect-error Tagged payloads preserve lossless integer types.
  const lossyPayment: Payment = { kind: "card", token: "test", sequence: 1 };
  void [futurePayment, mixedPayment, lossyPayment];
  const genericName: string = genericUser.data.name;
  const genericSequence: string = genericUser.data.sequence;
  const genericTitle: string = genericProject.data.title;
  void api.genericEcho({ body: genericUser.data });
  // @ts-expect-error Distinct generic instantiations retain their payload types.
  const wrongEnvelope: GenericUser = genericProject;
  // @ts-expect-error Model-owned IDs cannot cross generic payloads.
  const wrongGenericID: GenericUser = { ...genericUser, data: { ...genericUser.data, id: genericProject.data.id } };
  // @ts-expect-error Omission and explicit undefined remain different inside T.
  const wrongGenericPresence: GenericUser = { ...genericUser, data: { ...genericUser.data, note: undefined } };
  void [genericName, genericSequence, genericTitle, wrongEnvelope, wrongGenericID, wrongGenericPresence];
  void api.itemsEcho({ path: { key: "9223372036854775807" }, query: { q: "text", tag: ["one"] }, body: payload });
  void api.membersIndex({ query: { page: "1", per_page: "20" } });
  void api.membersSecure({ query: { page: "1", per_page: "20" } });
  void api.membersSecureSimple({ query: { per_page: "20" } });
  void api.membersSecureCursor({ query: { per_page: "20" } });
  // @ts-expect-error Authentication actors are not wire inputs.
  void api.membersSecure({ actor: { ID: "7" } });
  // @ts-expect-error Pagination integers retain the lossless wire type.
  void api.membersSecure({ query: { page: 1 } });
  // @ts-expect-error Cursor pagination cannot accept an offset page number.
  void api.membersSecureCursor({ query: { page: "1" } });
  const channel = realtime.channels.updates("7");
  channel.on.updated(value => { const large: string = value.large; void large; });
  channel.on.updated(async value => { const exact: string = value.exact; void exact; });
  void channel.publish.relay(payload, { onAccepted: () => {} });
  void realtime.channels.accounts("7").subscribe();

  // @ts-expect-error Wide integers cannot be JS numbers.
  void api.itemsEcho({ path: { key: 9223372036854775807 }, body: payload });
  // @ts-expect-error Required nullable is present even when its value is null.
  const missing: Payload = { ...payload, nullable: undefined };
  // @ts-expect-error Optional presence is different from explicit undefined.
  const undefinedOptional: Payload = { ...payload, optional: undefined };
  // @ts-expect-error Order identity cannot replace the buyer's identity.
  const crossOwner: Payload = { ...payload, buyer_id: payload.id };
  // @ts-expect-error Numeric exact values are decimal strings.
  const lossy: Payload = { ...payload, large: 1 };
  // @ts-expect-error An undeclared enum member is rejected.
  const wrongEnum: Payload = { ...payload, state: "missing" };
  // @ts-expect-error An upload is a Blob with an explicit filename.
  void api.uploadsProfile({ body: { document: "not a file" } });
  // @ts-expect-error Outgoing events cannot be published by clients.
  void channel.publish.updated(payload);
  // @ts-expect-error Incoming events cannot be received as publications.
  channel.on.relay(() => {});
  // @ts-expect-error Wrong event payload.
  void channel.publish.relay({ text: "wrong" });
  // @ts-expect-error Owned rooms require an explicit correctly typed identity.
  realtime.channels.accounts();
  // @ts-expect-error Narrowly typed API has no undeclared helper.
  void api.rawRoute({});
  if (echoed.state instanceof sdk.UnknownEnumValue) { const raw: string = echoed.state.value; void raw; }
  else { const known: Payload = { ...payload, state: echoed.state }; void known; }
  // @ts-expect-error A received unknown enum value must be narrowed before a request.
  const resent: Payload = { ...payload, state: echoed.state };
  void [missing, undefinedOptional, crossOwner, lossy, wrongEnum, resent];
}

export async function descriptorConsumer(): Promise<void> {
  const echo = sdk.operation("itemsEcho");
  const amount = echo.field("body", "amount");
  const decimal: sdk.FieldValue<typeof amount> = "12345678901234567890.12";
  const nullable = echo.field("body", "optional");
  const empty: sdk.FieldValue<typeof nullable> = null;
  const absent: sdk.FieldValue<typeof nullable> = undefined;
  const card = sdk.operation("unionsEcho").field("body", "method").variant("kind", "card");
  const token: sdk.FieldValue<ReturnType<typeof card.field<"token">>> = "token";
  // @ts-expect-error Only declared variants may be selected.
  sdk.operation("unionsEcho").field("body", "method").variant("kind", "future");
  // @ts-expect-error Select the variant before accessing its fields.
  sdk.operation("unionsEcho").field("body", "method").field("token");
  // @ts-expect-error Another variant's fields are unavailable.
  card.field("reference");
  void token;
  const tag = echo.field("body", "tags").element();
  const text: sdk.FieldValue<typeof tag> = "tag";
  const mapElement = sdk.schema("foundry.test/consumer/clientcontracts.ExplicitLabels").element();
  const mapText: sdk.FieldValue<typeof mapElement> = "value";
  // @ts-expect-error Absent map keys do not make present map element values undefined.
  const absentElement: sdk.FieldValue<typeof mapElement> = undefined;
  const nested = sdk.schema("foundry.test/consumer/clientcontracts.Payload").field("amount");
  const result: sdk.Operations["itemsEcho"]["response"] = await echo.call(api, { path: { key: "1" }, body: payload });
  const report: sdk.ValidationReport = echo.validate({ path: { key: "1" }, body: payload });
  // @ts-expect-error Unknown operation names are not descriptors.
  sdk.operation("missingOperation");
  // @ts-expect-error Field location belongs to the operation.
  sdk.operation("empty").field("body", "amount");
  // @ts-expect-error Unknown fields cannot be selected.
  echo.field("body", "privatePassword");
  // @ts-expect-error Path and body fields cannot be interchanged.
  echo.field("path", "amount");
  // @ts-expect-error Exact decimals cannot become floating point.
  const lossy: sdk.FieldValue<typeof amount> = 1.25;
  // @ts-expect-error Field owners cannot be interchanged with a reusable schema.
  const otherOwner: typeof amount = nested;
  // @ts-expect-error Different field value types and identities remain distinct.
  const otherField: typeof amount = echo.field("body", "state");
  // @ts-expect-error Primitive values expose no object fields.
  amount.field("length");
  // @ts-expect-error A scalar is not a collection.
  amount.element();
  // @ts-expect-error A DTO is not a collection.
  sdk.schema("foundry.test/consumer/clientcontracts.Payload").element();
  // @ts-expect-error Upload handles have no nested JSON fields.
  sdk.operation("uploadsProfile").field("body", "document").field("filename");
  // @ts-expect-error Descriptor call retains the exact required request.
  echo.call(api, { body: payload });
  // @ts-expect-error Descriptor metadata is read-only.
  echo.metadata.route.path = "/changed";
  void [decimal, empty, absent, text, mapText, absentElement, result, report, lossy, otherOwner, otherField];
}
