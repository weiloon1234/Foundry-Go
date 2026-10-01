import * as full from "./contracts_foundry.gen.js";
import * as live from "./contracts_live_foundry.gen.js";
import * as members from "./contracts_members_foundry.gen.js";

declare const memberAPI: members.API;
declare const liveAPI: live.API;
declare const memberPage: members.Operations["membersIndex"]["response"];
export function surfaceConsumer(): void {
  void memberAPI.membersIndex({});
  // @ts-expect-error A surface exposes only its selected operations.
  void memberAPI.accountShow({});
  void liveAPI.accountShow({});
  // @ts-expect-error Another portal's operation is not part of this surface.
  void liveAPI.membersIndex({});
  // Schemas keep their names and identity brands in every entry of a directory.
  const shared: full.Operations["membersIndex"]["response"] = memberPage;
  void shared;
  // Runtime classes come from the shared modules, so every entry has one class.
  const apiError: typeof full.APIError = members.APIError;
  void apiError;
  void live.createRealtime;
  // @ts-expect-error A surface without channels has no realtime client.
  void members.createRealtime;
}
