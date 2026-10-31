import assert from "node:assert/strict";
import { browserStorage } from "./browser-storage.mjs";
import { it } from "node:test";
import { pathToFileURL } from "node:url";

const auth = await import(pathToFileURL(process.argv[2]).href);
const { APIError } = await import(pathToFileURL(process.argv[3]).href);
const portal = await import(pathToFileURL(process.argv[4]).href);

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

function backend() {
  const sent = [];
  let reply = async path => {
    if (path === "/browser/profile") return { status: 200, payload: { display: "Visible member" } };
    if (path === "/browser/logout") return { status: 204 };
    return { status: 200, payload: { tokens: { access_token: "access", expires_in: 900, token_type: "Bearer" }, mfa_required: false } };
  };
  return {
    sent,
    respond: next => { reply = next; },
    transport: async request => {
      sent.push(request);
      const response = await reply(new URL(request.url, "https://fixture.test").pathname);
      return { status: response.status, headers: { "content-type": "application/json" }, body: response.payload === undefined ? "" : JSON.stringify(response.payload), close: () => undefined };
    },
  };
}

assert.equal(portal.signInIdentifier, "login_id");
assert.equal(portal.authDescriptor.identifier, "login_id");
assert.equal(portal.authDescriptor.guard, "browser_accounts");
assert.equal("cookieName" in portal.authDescriptor, false);

// Generated descriptor functions call the actual typed client operations.
{
  const api = backend();
  const instance = portal.createPortalSession(api.transport, { channel: null, now: () => 0, clientOptions: { baseURL: "https://fixture.test" } });
  assert.deepEqual({ ...(await instance.signInWithPassword({ login_id: "member@example.test", password: "fixture-password" })) }, { display: "Visible member" });
  assert.equal(instance.identifier, "login_id");
  assert.deepEqual(JSON.parse(api.sent[0].body), { login_id: "member@example.test", password: "fixture-password" });
  assert.equal(api.sent[0].headers.authorization, undefined);
  assert.equal(api.sent[1].headers.authorization, "Bearer access");
  assert.equal(instance.session.status(), "signed-in");
  await instance.signOut();
  assert.equal(new URL(api.sent[2].url).pathname, "/browser/logout");
  assert.equal(api.sent[2].headers.authorization, undefined);
  assert.equal(instance.session.status(), "signed-out");
  assert.equal(await instance.session.accessToken(), null);
  instance.session.dispose();
}

// Restore exchanges the refresh cookie on the plain client, then loads the profile.
{
  const api = backend();
  const instance = portal.createPortalSession(api.transport, { channel: null, now: () => 0 });
  assert.deepEqual({ ...(await instance.restoreSession()) }, { display: "Visible member" });
  assert.equal(api.sent[0].url, "/browser/refresh");
  assert.equal(api.sent[0].headers.authorization, undefined);
  assert.equal(api.sent[0].body, undefined);
  assert.equal(api.sent[1].headers.authorization, "Bearer access");
  // The injected clock also controls refresh-token expiry calculations.
  assert.equal(await instance.session.accessToken(), "access");
  assert.equal(api.sent.length, 2);
  instance.session.dispose();
}

// Failed login never adopts a token or requests a profile.
{
  const api = backend();
  api.respond(async () => ({ status: 401, payload: { status: 401, error_code: "unauthenticated", message: "Fixture denial" } }));
  const instance = portal.createPortalSession(api.transport, { channel: null });
  await assert.rejects(instance.signInWithPassword({ login_id: "member@example.test", password: "fixture-password" }), APIError);
  assert.equal(instance.session.status(), "signed-out");
  assert.equal(api.sent.length, 1);
  instance.session.dispose();
}

// A profile from an earlier sign-in cannot complete after a credential transition.
{
  const api = backend();
  const profile = deferred();
  const entered = deferred();
  api.respond(async path => {
    if (path === "/browser/profile") { entered.resolve(); return profile.promise; }
    return { status: 200, payload: { tokens: { access_token: "access", expires_in: 900, token_type: "Bearer" }, mfa_required: false } };
  });
  const instance = portal.createPortalSession(api.transport, { channel: null });
  const signingIn = instance.signInWithPassword({ login_id: "member@example.test", password: "fixture-password" });
  await entered.promise;
  instance.session.signOut();
  profile.resolve({ status: 200, payload: { display: "Old member" } });
  await assert.rejects(signingIn, auth.SessionChangedError);
  assert.equal(instance.session.status(), "signed-out");
  instance.session.dispose();
}

// M3: restoration must not publish a previous account's pending profile.
for (const transition of ["sign-out", "sign-in", "dispose"]) {
  const api = backend();
  const profile = deferred();
  const entered = deferred();
  api.respond(async path => {
    if (path === "/browser/profile") { entered.resolve(); return profile.promise; }
    return { status: 200, payload: { tokens: { access_token: "account-a", expires_in: 900, token_type: "Bearer" }, mfa_required: false } };
  });
  const instance = portal.createPortalSession(api.transport, { channel: null });
  const restoring = instance.restoreSession();
  await entered.promise;
  if (transition === "sign-out") instance.session.signOut();
  else if (transition === "sign-in") instance.session.signIn(auth.accessTokenExpiring("account-b", 900));
  else instance.session.dispose();
  profile.resolve({ status: 200, payload: { display: "Private account A" } });
  await assert.rejects(restoring, auth.SessionChangedError, `M3 ${transition}`);
  instance.session.dispose();
}

// M5: generated refresh calls receive owned cancellation, releasing the lock
// only once the transport has settled. A canceled rotation must not be retried.
for (const cancellation of ["timeout", "dispose"]) {
  const entered = deferred();
  let refreshRequest;
  let refreshes = 0;
  const transport = async request => {
    if (request.url !== "/browser/refresh") return { status: 204, headers: {}, body: "", close: () => undefined };
    refreshes++;
    refreshRequest = request;
    entered.resolve();
    return new Promise((resolve, reject) => {
      request.signal?.addEventListener("abort", () => reject(request.signal.reason), { once: true });
    });
  };
  const instance = portal.createPortalSession(transport, { channel: null, refreshTimeoutMs: 10 });
  const pending = instance.session.accessToken();
  await entered.promise;
  if (cancellation === "dispose") instance.session.dispose();
  // Bound the test itself so a missing signal cannot hang the test runner.
  const settled = await Promise.race([pending.then(() => true), new Promise(resolve => setTimeout(() => resolve(false), 100))]);
  assert.equal(settled, true, `M5 ${cancellation} must settle refresh`);
  assert.equal(refreshRequest.signal.aborted, true);
  let acquired = false;
  await instance.session.runCredentials(async () => { acquired = true; });
  assert.equal(acquired, true);
  assert.equal(await instance.session.accessToken(), null);
  assert.equal(refreshes, 1);
  instance.session.dispose();
}

console.log("portal auth descriptor and sessions ok");

// N1/M3: successful server credential changes must invalidate pending profiles
// even when completion metadata cannot be written; sibling tabs must fail closed.
for (const operation of ["signOut", "signInWithPassword"]) it(
  `N1/M3 rejects a superseded profile after ${operation} completion write fails`,
  async () => {
    const api = backend();
    const profile = deferred();
    const entered = deferred();
    const unavailable = new Error("storage unavailable");
    let transition = null;
    let fail = false;
    let remoteChanges = 0;
    let refreshes = 0;
    const coordination = {
      read: () => transition,
      write: next => { if (fail) throw unavailable; transition = next; },
    };
    const instance = portal.createPortalSession(api.transport, { channel: null, coordination, now: () => 0 });
    const sibling = portal.createPortalSession(api.transport, { channel: null, coordination, now: () => 0 });
    instance.session.signIn(auth.accessTokenExpiring("account-a", 900, () => 0));
    assert.equal(await sibling.session.accessToken(), "access");
    api.sent.length = 0;
    api.respond(async path => {
      if (path === "/browser/profile") { entered.resolve(); return profile.promise; }
      if (path === "/browser/refresh") refreshes++;
      else {
        assert.equal(transition.status, "uncertain", "N1 must persist uncertainty before sending a credential change");
        remoteChanges++;
        fail = true;
      }
      if (path === "/browser/logout") return { status: 204 };
      return { status: 200, payload: { tokens: { access_token: "account-b", expires_in: 900, token_type: "Bearer" }, mfa_required: false } };
    });
    const restoring = instance.restoreSession();
    await entered.promise;
    assert.equal(api.sent[0].headers.authorization, "Bearer account-a");
    await assert.rejects(instance[operation]({ login_id: "b@example.test", password: "fixture-password" }), auth.SessionCoordinationError);
    assert.equal(remoteChanges, 1);
    profile.resolve({ status: 200, payload: { display: "Private account A" } });
    await assert.rejects(restoring, auth.SessionChangedError, `M3 ${operation} completion write failure`);
    assert.equal(instance.session.status(), "signed-out");
    await assert.rejects(instance.session.accessToken(), auth.SessionCoordinationError);
    assert.equal(transition.status, "uncertain", "N1 shared identity must stop describing account A as active");
    assert.deepEqual(Object.keys(transition).sort(), ["identity", "revision", "status"]);
    assert.equal(await sibling.session.accessToken(), null);
    const newcomer = portal.createPortalSession(api.transport, { channel: null, coordination, now: () => 0 });
    assert.equal(await newcomer.session.accessToken(), null);
    assert.equal(refreshes, 0);
    instance.session.dispose();
    sibling.session.dispose();
    newcomer.session.dispose();
  },
);

for (const operation of ["signOut", "signInWithPassword"]) it(
  `N1 refuses ${operation} when its shared uncertainty marker cannot be written`,
  async () => {
    const api = backend();
    let transition = null;
    let fail = false;
    const unavailable = new Error("storage unavailable");
    const instance = portal.createPortalSession(api.transport, {
      channel: null, now: () => 0,
      coordination: {
        read: () => transition,
        write: next => { if (fail) throw unavailable; transition = next; },
      },
    });
    instance.session.signIn(auth.accessTokenExpiring("account-a", 900, () => 0));
    fail = true;
    await assert.rejects(instance[operation]({ login_id: "b@example.test", password: "fixture-password" }), auth.SessionCoordinationError);
    assert.equal(api.sent.length, 0);
    assert.equal(instance.session.status(), "signed-out");
    await assert.rejects(instance.session.accessToken(), auth.SessionCoordinationError);
    instance.session.dispose();
  },
);

for (const denial of ["getter", "read", "write"]) {
  it(`portal renders with ${denial} denied and rejects credential operations`, async t => {
    const storage = browserStorage(t);
    storage.deny(denial);
    const api = backend();
    const instance = portal.createPortalSession(api.transport, { channel: null, now: () => 0 });
    t.after(() => instance.session.dispose());
    assert.equal(storage.accesses(), 0);
    assert.equal(instance.session.status(), "unknown");
    assert.doesNotThrow(() => instance.session.version());
    await assert.rejects(instance.restoreSession(), auth.SessionCoordinationError);
    await assert.rejects(instance.signInWithPassword({ login_id: "member@example.test", password: "fixture-password" }), auth.SessionCoordinationError);
    assert.equal(instance.session.status(), "signed-out");
    assert.equal(api.sent.length, 0, "no credential mutation may be sent without coordination");
  });

  for (const operation of ["signInWithPassword", "restoreSession"]) it(
    `portal ${operation} clears credentials when ${denial} is denied during the response`,
    async t => {
      const storage = browserStorage(t);
      const api = backend();
      const instance = portal.createPortalSession(api.transport, { channel: null, now: () => 0 });
      t.after(() => instance.session.dispose());
      instance.session.signIn(auth.accessTokenExpiring("old-bearer", 900, () => 0));
      if (operation === "restoreSession") instance.session.rejected("old-bearer");
      api.respond(async () => {
        storage.deny(denial);
        return { status: 200, payload: { tokens: { access_token: "new-bearer", expires_in: 900, token_type: "Bearer" }, mfa_required: false } };
      });
      await assert.rejects(instance[operation]({ login_id: "member@example.test", password: "fixture-password" }), auth.SessionCoordinationError);
      assert.equal(instance.session.status(), "signed-out");
      assert.equal(api.sent.length, 1, "failed publication must not fetch a profile");
      storage.deny(null);
      await assert.rejects(instance.session.accessToken(), auth.SessionCoordinationError);
    },
  );
}
