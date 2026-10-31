import * as sdk from "./contracts_foundry.gen.js";

type Member = sdk.ContractTypes["foundry.test/consumer/reporting.MemberRow"];
type NumericLevel = sdk.ContractTypes["foundry.test/consumer/reporting.NumericLevelRow"];
type Order = sdk.ContractTypes["foundry.test/consumer/reporting.OrderRow"];
type Total = sdk.ContractTypes["foundry.test/consumer/reporting.MemberTotal"];
declare const member: Member;
declare const numericLevel: NumericLevel;
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
  // S2: typed datatable descriptor and query/export helpers
  const membersTable = sdk.tables["reports.members"];
  const tableMember: sdk.TableRow<"reports.members"> = member;
  const tableNumericLevel: sdk.TableRow<"reports.numeric_levels"> = numericLevel;
  const numericLevelsTable = sdk.tables["reports.numeric_levels"];
  for (const column of numericLevelsTable.columns) {
    if (column.kind === "enum" && column.name === "level") {
      for (const level of column.enum.cases) {
        const value: 0 | 1 = level;
        // @ts-expect-error Integer enum cases remain numbers.
        const text: string = level;
        void [value, text];
      }
      // @ts-expect-error Cases belong to the enum value object.
      void column.cases;
    }
    if (column.kind === "enum" && column.name === "wideLevel") {
      for (const level of column.enum.cases) {
        const value: "9007199254740993" | "9007199254740994" = level;
        // @ts-expect-error Wide integer enum cases preserve decimal strings.
        const rounded: number = level;
        void [value, rounded];
      }
    }
  }
  void sdk.tableQueries["reports.members"](api, { body: {} });
  void sdk.tableDownloads["reports.members"](api, {});
  // @ts-expect-error Capability alone does not declare an export operation.
  void sdk.tableExports["reports.members"](api, {});
  // @ts-expect-error Filters keep their declared operator set.
  void api.reportsMembersQuery({ body: { filters: [{ op: "raw_sql" }] } });
  void [amount, count, sum, note, memberID, wrongID, rounded, countNumber, tableMember, tableNumericLevel];
}
