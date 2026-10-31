import assert from "node:assert/strict";
import { describe, it, mock } from "node:test";
import { pathToFileURL } from "node:url";

const { accessTokenExpiring, createTokenSession } = await import(pathToFileURL(process.argv[2]).href);

/** One exclusive lock shared by the tabs of a test, like Web Locks. */
function sharedLock() {
  let tail = Promise.resolve();
  let transition = null;
  return {
    coordination: {
      read: () => transition,
      write: next => { transition = next; },
    },
    run(_name, work) {
      const result = tail.then(work);
      tail = result.catch(() => undefined);
      return result;
    },
  };
}

/** Delivers each tab's messages to the other tabs, asynchronously. */
function sharedChannel(delayed = false) {
  const pending = [];
  const receivers = new Set();
  const channel = () => {
    let own;
    return {
      post(message) {
        for (const receive of receivers)
          if (receive !== own) {
            const deliver = () => receive(message);
            if (delayed) pending.push(deliver);
            else queueMicrotask(deliver);
          }
      },
      listen(receive) {
        own = receive;
        receivers.add(receive);
        return () => receivers.delete(receive);
      },
    };
  };
  channel.deliver = () => { for (const receive of pending.splice(0)) receive(); };
  return channel;
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
    coordination: shared.lock.coordination,
    channel: shared.channel(),
  });
}

describe("createTokenSession", () => {
  it("M1 persists only transition metadata and blocks another browser tab without messages", async () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, "window");
    const stored = new Map();
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: { localStorage: {
        getItem: key => stored.get(key) ?? null,
        setItem: (key, value) => stored.set(key, value),
      } },
    });
    const lock = sharedLock();
    const one = createTokenSession({ name: "browser", lock, channel: null, refresh: async () => { throw new TypeError("lost response"); } });
    const retry = mock.fn(async () => renewed(at("unsafe", Date.now() + 60_000)));
    const two = createTokenSession({ name: "browser", lock, channel: null, refresh: retry });
    try {
      await one.runCredentials(async () => one.signIn(at("memory-only", 0)));
      assert.equal(await one.accessToken(), null);
      assert.equal(await two.accessToken(), null);
      assert.equal(retry.mock.callCount(), 0);
      assert.equal(stored.size, 1);
      const record = JSON.parse([...stored.values()][0]);
      assert.deepEqual(Object.keys(record).sort(), ["identity", "revision", "status"]);
      assert.equal(record.status, "uncertain");
      stored.set("foundry.browser.session.transition", "invalid");
      assert.throws(() => two.accessToken(), SyntaxError);
    } finally {
      one.dispose();
      two.dispose();
      if (original === undefined) delete globalThis.window;
      else Object.defineProperty(globalThis, "window", original);
    }
  });

  it("M1 refuses refresh if the shared uncertainty marker cannot be written", async () => {
    const refresh = mock.fn(async () => renewed(at("unsafe", 60_000)));
    const unavailable = new Error("storage unavailable");
    const session = createTokenSession({
      name: "storage-failure", channel: null, refresh,
      coordination: { read: () => null, write: () => { throw unavailable; } },
    });
    await assert.rejects(session.accessToken(), error => error === unavailable);
    assert.equal(refresh.mock.callCount(), 0);
    session.dispose();
  });

  it("M1 blocks queued and newly opened tabs after an unknown refresh outcome", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel(true) };
    const first = tab(async () => { throw new TypeError("lost rotation response"); }, shared);
    const retry = mock.fn(async () => renewed(at("unsafe-retry", 60_000)));
    const second = tab(retry, shared);
    assert.deepEqual(await Promise.all([first.accessToken(), second.accessToken()]), [null, null]);
    const third = tab(retry, shared);
    assert.equal(await third.accessToken(), null);
    assert.equal(retry.mock.callCount(), 0);
    await second.runCredentials(async () => second.signIn(at("new-login", 60_000)));
    assert.equal(await second.accessToken(), "new-login");
    for (const session of [first, second, third]) session.dispose();
  });

  for (const transition of ["signed-out", "signed-in", "token"]) it(
    `M2 ignores delayed ${transition} messages after a newer login`,
    async () => {
      const shared = { lock: sharedLock(), channel: sharedChannel(true) };
      const first = tab(async () => renewed(at("old-refresh", 120_000)), shared);
      const second = tab(async () => renewed(at("new-renewal", 60_000)), shared);
      if (transition === "token") await first.accessToken();
      else await first.runCredentials(async () => {
        if (transition === "signed-out") first.signOut();
        else first.signIn(at("old-login", 120_000));
      });
      await second.runCredentials(async () => second.signIn(at("new-login", 60_000)));
      const version = second.version();
      if (transition === "token") second.rejected("new-login");
      shared.channel.deliver();
      assert.equal(await second.accessToken(), transition === "token" ? "new-renewal" : "new-login");
      assert.equal(second.version(), version);
      assert.equal(await first.accessToken(), "new-login");
      first.dispose();
      second.dispose();
    },
  );

  for (const operation of ["signIn", "signOut"]) it(
    `N1 invalidates local credentials when ${operation} cannot publish metadata`,
    async () => {
      const lock = sharedLock();
      let fail = false;
      const unavailable = new Error("storage unavailable");
      const refresh = mock.fn(async () => renewed(at("unsafe", 60_000)));
      const session = createTokenSession({
        name: "failed-transition", channel: null, now: () => 0, lock, refresh,
        coordination: {
          read: lock.coordination.read,
          write: next => { if (fail) throw unavailable; lock.coordination.write(next); },
        },
      });
      session.signIn(at("account-a", 60_000));
      const version = session.version();
      fail = true;
      assert.throws(() => session[operation](at("account-b", 60_000)), error => error === unavailable);
      assert.equal(session.status(), "signed-out", `N1 ${operation} local invalidation`);
      assert.notEqual(session.version(), version);
      assert.equal(await session.accessToken(), null);
      assert.equal(refresh.mock.callCount(), 0);
      fail = false;
      assert.equal(await session.accessToken(), null, "storage recovery must not revive the old identity");
      session.signIn(at("new-login", 60_000));
      assert.equal(await session.accessToken(), "new-login");
      session.dispose();
    },
  );

  it("N2 notifies an account change when refresh supersedes its sign-in message", async () => {
    const shared = { lock: sharedLock(), channel: sharedChannel(true) };
    const one = tab(async () => renewed(at("account-b-refresh", 60_000)), shared);
    const two = tab(async () => ({ kind: "signed-out" }), shared);
    await one.runCredentials(async () => one.signIn(at("account-a", 60_000)));
    shared.channel.deliver();
    const version = two.version();
    const statuses = [];
    two.subscribe(status => statuses.push(status));
    await one.runCredentials(async () => one.signIn(at("account-b", 60_000)));
    one.rejected("account-b");
    assert.equal(await one.accessToken(), "account-b-refresh");
    shared.channel.deliver();
    assert.equal(await two.accessToken(), "account-b-refresh");
    assert.notEqual(two.version(), version);
    assert.deepEqual(statuses, ["signed-in"], "N2 account change must notify even without a status change");
    one.rejected("account-b-refresh");
    await one.accessToken();
    shared.channel.deliver();
    assert.deepEqual(statuses, ["signed-in"], "same-identity renewal must remain silent");
    one.dispose();
    two.dispose();
  });

  it("M5 bounds time queued for the credential lock without starting refresh", async () => {
    const lock = sharedLock();
    const release = Promise.withResolvers();
    const holder = lock.run("test", () => release.promise);
    const refresh = mock.fn(async () => renewed(at("unused", 60_000)));
    const session = createTokenSession({ name: "queued", channel: null, lock, refresh, refreshTimeoutMs: 10 });
    try {
      const pending = session.accessToken();
      assert.equal(await Promise.race([pending.then(() => "settled"), new Promise(resolve => setTimeout(() => resolve("pending"), 100))]), "settled");
      assert.equal(refresh.mock.callCount(), 0);
    } finally {
      session.dispose();
      release.resolve();
      await holder;
    }
  });

  it("M5 bounds refresh waits without releasing an uncooperative credential writer", async () => {
    const entered = Promise.withResolvers();
    const finish = Promise.withResolvers();
    const lock = sharedLock();
    const session = createTokenSession({
      name: "uncooperative",
      channel: null,
      lock,
      coordination: lock.coordination,
      refreshTimeoutMs: 10,
      refresh: () => { entered.resolve(); return finish.promise; },
    });
    const pending = session.accessToken();
    await entered.promise;
    let acquired = false;
    const login = session.runCredentials(async () => {
      acquired = true;
      session.signIn(at("new-login", Date.now() + 60_000));
    });
    try {
      assert.equal(await Promise.race([pending.then(() => "settled"), new Promise(resolve => setTimeout(() => resolve("pending"), 100))]), "settled");
      assert.equal(acquired, false);
    } finally {
      finish.resolve(renewed(at("late-refresh", Date.now() + 60_000)));
      await login;
      session.dispose();
    }
  });

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
