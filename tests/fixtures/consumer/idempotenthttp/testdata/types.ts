import { type API, type IdempotencyKey, type Operations, idempotencyKey } from "./contracts_foundry.gen.js";
declare const client: API;
const key: IdempotencyKey = idempotencyKey("typescript-submission-0001");
void client.ordersCreate({ path: { workspace: "1" }, body: { name: "valid" }, idempotencyKey: key });
// @ts-expect-error required submission key is part of this operation's request
void client.ordersCreate({ path: { workspace: "1" }, body: { name: "missing" } });
// @ts-expect-error arbitrary string is not a validated submission key
void client.ordersCreate({ path: { workspace: "1" }, body: { name: "wrong" }, idempotencyKey: "raw" });
// @ts-expect-error request body retains its concrete generated field types
void client.ordersCreate({ path: { workspace: "1" }, body: { name: 12 }, idempotencyKey: key });
const outcome: Operations["ordersCreate"]["errorCode"] = "idempotency_mismatch";
void outcome;
// @ts-expect-error undocumented failure code is absent from the actual endpoint
const wrong: Operations["ordersCreate"]["errorCode"] = "unknown_response";
void wrong;
