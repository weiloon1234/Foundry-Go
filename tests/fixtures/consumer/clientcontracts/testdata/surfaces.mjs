import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const [full, members, live] = await Promise.all(process.argv.slice(2, 5).map(path => import(pathToFileURL(path).href)));
const baseURL = process.argv[5];
const transport = async request => {
  const result = await fetch(request.url, { method: request.method, headers: request.headers, body: request.body, signal: request.signal, credentials: request.credentials, redirect: request.redirect, ...(request.duplex ? { duplex: request.duplex } : {}) });
  return { status: result.status, headers: Object.fromEntries(result.headers), body: result.body ?? new Uint8Array(), close: async () => { if (result.body && !result.body.locked) await result.body.cancel(); } };
};

// A surface client calls its own operations against the real server.
const api = members.createClient(transport, { baseURL });
const page = await api.membersIndex({});
assert.equal(page.meta.per_page, "20"); assert.deepEqual(page.data, []);
assert.equal("accountShow" in api, false);
const account = live.createClient(transport, { baseURL });
await assert.rejects(account.accountShow({}), error => error instanceof live.APIError && error.status === 401);
// Runtime classes are shared by every entry in the directory.
assert.equal(members.APIError, full.APIError);
assert.equal(live.ContractError, full.ContractError);
// Each entry embeds only its projection of the manifest.
const membersMetadata = members.contractMetadata(), fullMetadata = full.contractMetadata();
assert.ok(membersMetadata.http.length > 0 && membersMetadata.http.every(op => op.route.id.startsWith("members.")));
assert.ok(membersMetadata.http.length < fullMetadata.http.length);
assert.equal(membersMetadata.realtime, undefined); assert.equal(members.createRealtime, undefined);
assert.deepEqual(live.contractMetadata().realtime.channels.map(channel => channel.id).sort(), ["accounts", "updates"]);
assert.ok(members.manifestJSON.length < full.manifestJSON.length);

// The realtime runtime serves a surface with channels.
const native = new WebSocket(baseURL.replace(/^http/, "ws") + "/ws", live.realtimeSubprotocol);
await new Promise((resolve, reject) => { native.addEventListener("open", resolve, { once: true }); native.addEventListener("error", () => reject(new Error("native socket failed")), { once: true }); });
const realtime = live.createRealtime({ protocol: native.protocol, send: text => native.send(text), close: () => native.close(), listen(receive, closed) {
  const message = event => receive(event.data), close = () => closed();
  native.addEventListener("message", message); native.addEventListener("close", close);
  return () => { native.removeEventListener("message", message); native.removeEventListener("close", close); };
} });
const room = realtime.channels.updates("7");
assert.deepEqual(await room.subscribe(), []);
await room.unsubscribe(); room.dispose(); realtime.close();
console.log("surface clients ok");
