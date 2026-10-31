import assert from "node:assert/strict";
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
  assert.equal(instance.session.status(), "unknown");
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

console.log("portal auth descriptor and sessions ok");
