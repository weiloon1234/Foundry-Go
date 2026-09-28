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
export function typedConsumer(): void {
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
  void [missing, undefinedOptional, crossOwner, lossy, wrongEnum];
}
