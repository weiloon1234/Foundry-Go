import * as sdk from "./contracts_foundry.gen.js";

type Member = sdk.ContractTypes["foundry.test/consumer/reporting.MemberRow"];
type Order = sdk.ContractTypes["foundry.test/consumer/reporting.OrderRow"];
type Total = sdk.ContractTypes["foundry.test/consumer/reporting.MemberTotal"];
declare const member: Member;
declare const order: Order;
declare const total: Total;
declare const api: sdk.API;
export function reportingTypes(): void {
  const amount: string = order.amount;
  const count: string = total.count;
  const sum: string | null = total.total;
  const note: string | null = member.nickname;
  const memberID: Member["id"] = total.memberId;
  // @ts-expect-error Order and member identities remain distinct.
  const wrongID: Member["id"] = order.id;
  // @ts-expect-error An exact total cannot become a floating-point number.
  const rounded: number = total.total;
  // @ts-expect-error Aggregate count is lossless, not a JavaScript number.
  const countNumber: number = total.count;
  void api.reportsMembersQuery({ body: { filters: [{ op: "eq", column: "state", values: ["active"] }] } });
  void api.reportsMembersCsv({});
  // @ts-expect-error Filters keep their declared operator set.
  void api.reportsMembersQuery({ body: { filters: [{ op: "raw_sql" }] } });
  void [amount, count, sum, note, memberID, wrongID, rounded, countNumber];
}
