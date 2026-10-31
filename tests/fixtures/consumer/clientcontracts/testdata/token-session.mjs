import assert from "node:assert/strict";
import { describe, it, mock } from "node:test";
import { pathToFileURL } from "node:url";

const { accessTokenExpiring, createTokenSession } = await import(pathToFileURL(process.argv[2]).href);

/** One exclusive lock shared by the tabs of a test, like Web Locks. */
function sharedLock() {
  let tail = Promise.resolve();
  return {
    run(_name, work) {
      const result = tail.then(work);
      tail = result.catch(() => undefined);
      return result;
    },
  };
}

/** Delivers each tab's messages to the other tabs, asynchronously. */
function sharedChannel() {
  const receivers = new Set();
  return () => {
    let own;
    return {
      post(message) {
        for (const receive of receivers)
          if (receive !== own) queueMicrotask(() => receive(message));
      },
      listen(receive) {
        own = receive;
        receivers.add(receive);
        return () => receivers.delete(receive);
      },
    };
  };
}

const at = (value, expiresAt) => ({
  value,
  expiresAt,
});
const renewed = (token) => ({
  kind: "renewed",
  token,
});

function tab(
  refresh,
  shared,
  now = () => 0,
) {
  return createTokenSession({
    name: "test",
    refresh,
    now,
    lock: shared.lock,
    channel: shared.channel(),
  });
}

describe("createTokenSession", () => {
  it("serializes login cookie writes behind an in-flight refresh", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel() };
    let finish;
    const first = tab(
      () => new Promise((resolve) => (finish = resolve)),
      shared,
    );
    const second = tab(async () => ({ kind: "signed-out" }), shared);
    const refresh = first.accessToken();
    await Promise.resolve();
    const login = mock.fn(async () => second.signIn(at("new", 60_000)));
    const signingIn = second.runCredentials(login);
    await Promise.resolve();
    assert.equal(login.mock.callCount(), 0);
    finish(renewed(at("old", 120_000)));
    await refresh;
    await signingIn;
    await Promise.resolve();
    assert.equal(await first.accessToken(), "new");
    assert.equal(await second.accessToken(), "new");
  });

  it("accepts a newer login even when its token expires sooner", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel() };
    const one = tab(async () => ({ kind: "signed-out" }), shared);
    const two = tab(async () => ({ kind: "signed-out" }), shared);
    one.signIn(at("old", 120_000));
    await Promise.resolve();
    two.signIn(at("new", 60_000));
    await Promise.resolve();
    assert.equal(await one.accessToken(), "new");
  });

  it("ignores a refresh arriving after disposal", async () => {
    let finish;
    const session = tab(
      () => new Promise((resolve) => (finish = resolve)),
      { lock: sharedLock(), channel: sharedChannel() },
    );
    const pending = session.accessToken();
    await Promise.resolve();
    session.dispose();
    finish(renewed(at("disposed", 60_000)));
    assert.equal(await pending, null);
    assert.equal(await session.accessToken(), null);
  });

  for (const origin of ["local", "other tab"]) it(
    `discards a refresh finishing after ${origin} sign-out`,
    async () => {
      const shared = { lock: sharedLock(), channel: sharedChannel() };
      let finish;
      const refresh = mock.fn(
        () => new Promise((resolve) => (finish = resolve)),
      );
      const session = tab(refresh, shared);
      const other = tab(async () => ({ kind: "signed-out" }), shared);
      const pending = session.accessToken();
      await Promise.resolve();
      if (origin === "local") session.signOut();
      else other.signOut();
      await Promise.resolve();
      finish(renewed(at("revoked", 60_000)));
      assert.equal(await pending, null);
      assert.equal(session.status(), "signed-out");
      assert.equal(await session.accessToken(), null);
      assert.equal(other.status(), "signed-out");
    },
  );

  for (const outcome of ["renewed", "signed-out", "failed"]) it(
    `ignores an old ${outcome} refresh after a new sign-in`,
    async () => {
      let finish;
      let fail;
      const session = tab(
        () =>
          new Promise((resolve, reject) => {
            finish = resolve;
            fail = reject;
          }),
        { lock: sharedLock(), channel: sharedChannel() },
      );
      const pending = session.accessToken();
      await Promise.resolve();
      session.signIn(at("new-login", 60_000));
      if (outcome === "failed") fail(new TypeError("network"));
      else
        finish(
          outcome === "renewed"
            ? renewed(at("old-login", 120_000))
            : { kind: "signed-out" },
        );
      assert.equal(await pending, null);
      assert.equal(session.status(), "signed-in");
      assert.equal(await session.accessToken(), "new-login");
    },
  );

  it("does not start a queued refresh after sign-out", async () => {
    const refresh = mock.fn(async () => renewed(at("revoked", 60_000)));
    const session = tab(refresh, {
      lock: sharedLock(),
      channel: sharedChannel(),
    });
    const pending = session.accessToken();
    session.signOut();
    assert.equal(await pending, null);
    assert.equal(refresh.mock.callCount(), 0);
  });

  it("renews once for concurrent callers", async () => {
    const refresh = mock.fn(async () => renewed(at("a1", 60_000)));
    const session = tab(refresh, {
      lock: sharedLock(),
      channel: sharedChannel(),
    });
    const tokens = await Promise.all(
      Array.from({ length: 5 }, () => session.accessToken()),
    );
    assert.deepEqual(tokens, ["a1", "a1", "a1", "a1", "a1"]);
    assert.equal(refresh.mock.callCount(), 1);
    assert.equal(session.status(), "signed-in");
  });

  it("reuses a fresh token and renews within the margin before expiry", async () => {
    let clock = 0;
    const outcomes = [renewed(at("a1", 60_000)), renewed(at("a2", 120_000))];
    const refresh = mock.fn(async () => outcomes.shift());
    const session = tab(
      refresh,
      { lock: sharedLock(), channel: sharedChannel() },
      () => clock,
    );
    assert.equal(await session.accessToken(), "a1");
    clock = 29_000;
    assert.equal(await session.accessToken(), "a1");
    clock = 31_000;
    assert.equal(await session.accessToken(), "a2");
    assert.equal(refresh.mock.callCount(), 2);
  });

  it("lets a tab waiting for the lock adopt another tab's renewal", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel() };
    const first = mock.fn(async () => renewed(at("a1", 60_000)));
    const second = mock.fn(async () => renewed(at("b1", 60_000)));
    const one = tab(first, shared);
    const two = tab(second, shared);
    const [a, b] = await Promise.all([one.accessToken(), two.accessToken()]);
    assert.deepEqual([a, b], ["a1", "a1"]);
    assert.equal(first.mock.callCount(), 1);
    assert.equal(second.mock.callCount(), 0);
  });

  it("signs every tab out when the server rejects the refresh credential", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel() };
    const one = tab(async () => ({ kind: "signed-out" }), shared);
    const otherRefresh = mock.fn(async () => renewed(at("b1", 60_000)));
    const two = tab(otherRefresh, shared);
    assert.equal(await one.accessToken(), null);
    await Promise.resolve();
    assert.equal(one.status(), "signed-out");
    assert.equal(two.status(), "signed-out");
    assert.equal(await two.accessToken(), null);
    assert.equal(otherRefresh.mock.callCount(), 0);
  });

  it("never resends a refresh whose outcome is unknown", async () => {
    const refresh = mock.fn(async () => {
      throw new TypeError("network");
    });
    const session = tab(refresh, {
      lock: sharedLock(),
      channel: sharedChannel(),
    });
    assert.equal(await session.accessToken(), null);
    assert.equal(await session.accessToken(), null);
    assert.equal(refresh.mock.callCount(), 1);
    assert.equal(session.status(), "signed-out");
  });

  it("renews a rejected token but keeps a newer one", async () => {
    const refresh = mock.fn(async () => renewed(at("a2", 60_000)));
    const session = tab(refresh, {
      lock: sharedLock(),
      channel: sharedChannel(),
    });
    session.signIn(at("a1", 60_000));
    session.rejected("stale");
    assert.equal(await session.accessToken(), "a1");
    session.rejected("a1");
    assert.equal(await session.accessToken(), "a2");
    assert.equal(refresh.mock.callCount(), 1);
  });

  it("shares sign-in and sign-out with the portal's other tabs", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel() };
    const refresh = mock.fn(async () => renewed(at("x", 60_000)));
    const one = tab(refresh, shared);
    const two = tab(refresh, shared);
    const statuses = [];
    two.subscribe((status) => statuses.push(status));
    one.signIn(at("a1", 60_000));
    await Promise.resolve();
    assert.equal(await two.accessToken(), "a1");
    one.signOut();
    await Promise.resolve();
    assert.equal(two.status(), "signed-out");
    assert.deepEqual(statuses, ["signed-in", "signed-out"]);
    assert.equal(refresh.mock.callCount(), 0);
  });

  it("computes expiry from expires_in seconds", () => {
    assert.deepEqual(accessTokenExpiring("a", 900, () => 1_000), {
      value: "a",
      expiresAt: 901_000,
    });
  });
});
