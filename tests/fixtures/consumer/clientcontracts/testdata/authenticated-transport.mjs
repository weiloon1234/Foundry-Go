import assert from "node:assert/strict";
import { describe, it, mock } from "node:test";
import { pathToFileURL } from "node:url";

const { authenticatedTransport, createTokenSession } = await import(pathToFileURL(process.argv[2]).href);

function request(overrides = {}) {
  return {
    method: "POST",
    url: "/api/user/v1/projects",
    headers: { "content-type": "application/json" },
    body: "{}",
    credentials: "same-origin",
    redirect: "follow",
    maxBodyBytes: 1024,
    ...overrides,
  };
}

function backend(...statuses) {
  const sent = [];
  const closed = [];
  const transport = async (sentRequest) => {
    const index = sent.push(sentRequest) - 1;
    return {
      status: statuses[index] ?? 200,
      headers: {},
      body: "",
      close: () => {
        closed.push(index);
      },
    };
  };
  return { transport, sent, closed };
}

/** A session that hands out the given tokens in order. */
function session(...tokens) {
  const rejected = [];
  let index = 0;
  const value = {
    status: () => "signed-in",
    version: () => 0,
    runCredentials: (work) => work(),
    subscribe: () => () => undefined,
    accessToken: mock.fn(
      async () => tokens[Math.min(index, tokens.length - 1)] ?? null,
    ),
    signIn: () => undefined,
    rejected: (token) => {
      rejected.push(token);
      index++;
    },
    signOut: () => undefined,
    dispose: () => undefined,
  };
  return { value, rejected };
}

const authorization = (sent) => sent.headers["authorization"];

describe("authenticatedTransport", () => {
  it("N1 never sends the previous bearer after a failed identity publication", async () => {
    let transition = null;
    let fail = false;
    const unavailable = new Error("storage unavailable");
    const tokens = createTokenSession({
      name: "failed-login", channel: null,
      refresh: async () => { throw new Error("must not refresh the mismatched cookie"); },
      coordination: {
        read: () => transition,
        write: next => { if (fail) throw unavailable; transition = next; },
      },
    });
    tokens.signIn({ value: "account-a", expiresAt: Date.now() + 60_000 });
    fail = true;
    assert.throws(() => tokens.signIn({ value: "account-b", expiresAt: Date.now() + 60_000 }), error => error === unavailable);
    const api = backend(200);
    await authenticatedTransport(api.transport, tokens)(request());
    assert.deepEqual(api.sent.map(authorization), [undefined], "N1 old bearer must not escape through transport");
    tokens.dispose();
  });

  it("M5 cancels one refresh waiter without canceling the shared renewal", async () => {
    let finish;
    const entered = Promise.withResolvers();
    const refresh = mock.fn(() => {
      entered.resolve();
      return new Promise(resolve => { finish = resolve; });
    });
    const tokens = createTokenSession({ name: "cancel-wait", refresh, channel: null });
    const api = backend(200);
    const call = authenticatedTransport(api.transport, tokens);
    const controller = new AbortController();
    let result = "pending";
    const canceled = call(request({ signal: controller.signal })).then(
      () => { result = "resolved"; },
      error => { assert.equal(error, controller.signal.reason); result = "aborted"; },
    );
    await entered.promise;
    const survivor = call(request());
    controller.abort();
    await new Promise(resolve => setImmediate(resolve));
    try {
      assert.equal(result, "aborted");
      assert.equal(api.sent.length, 0);
    } finally {
      finish({ kind: "renewed", token: { value: "shared", expiresAt: Date.now() + 60_000 } });
      await Promise.all([canceled, survivor]);
      tokens.dispose();
    }
    assert.equal(refresh.mock.callCount(), 1);
    assert.equal(api.sent.length, 1);
  });

  it("M5 closes a discarded 401 when its renewal wait is canceled", async () => {
    const entered = Promise.withResolvers();
    const finish = Promise.withResolvers();
    const tokens = createTokenSession({
      name: "cancel-retry", channel: null,
      refresh: () => { entered.resolve(); return finish.promise; },
    });
    tokens.signIn({ value: "old", expiresAt: Date.now() + 60_000 });
    const api = backend(401);
    const controller = new AbortController();
    const pending = authenticatedTransport(api.transport, tokens)(request({ signal: controller.signal }));
    await entered.promise;
    controller.abort();
    try {
      await assert.rejects(pending, error => error === controller.signal.reason);
      assert.deepEqual(api.closed, [0]);
      assert.equal(api.sent.length, 1);
    } finally {
      finish.resolve({ kind: "signed-out" });
      tokens.dispose();
    }
  });

  it("does not replay an old mutation using a later login", async () => {
    const tokens = session("old", "new");
    let version = 1;
    tokens.value.version = () => version;
    const base = mock.fn(async () => {
      version++;
      return { status: 401, headers: {}, body: "", close: () => undefined };
    });
    const response = await authenticatedTransport(
      base,
      tokens.value,
    )(request());
    assert.equal(response.status, 401);
    assert.equal(base.mock.callCount(), 1);
    assert.deepEqual(tokens.rejected, []);
  });

  it("sends the current access token", async () => {
    const api = backend(200);
    const response = await authenticatedTransport(
      api.transport,
      session("a1").value,
    )(request());
    assert.equal(response.status, 200);
    assert.deepEqual(api.sent.map(authorization), ["Bearer a1"]);
  });

  it("renews once after a 401 and resends the request once", async () => {
    const api = backend(401, 200);
    const tokens = session("a1", "a2");
    const response = await authenticatedTransport(
      api.transport,
      tokens.value,
    )(request());
    assert.equal(response.status, 200);
    assert.deepEqual(api.sent.map(authorization), ["Bearer a1", "Bearer a2"]);
    assert.deepEqual(tokens.rejected, ["a1"]);
    assert.deepEqual(api.closed, [0]);
  });

  it("returns a second 401 instead of retrying again", async () => {
    const api = backend(401, 401);
    const response = await authenticatedTransport(
      api.transport,
      session("a1", "a2").value,
    )(request());
    assert.equal(response.status, 401);
    assert.equal(api.sent.length, 2);
  });

  it("returns the 401 when renewal ends the session", async () => {
    const api = backend(401);
    const response = await authenticatedTransport(
      api.transport,
      session("a1", null).value,
    )(request());
    assert.equal(response.status, 401);
    assert.equal(api.sent.length, 1);
    assert.deepEqual(api.closed, []);
  });

  it("never resends a streamed body", async () => {
    const api = backend(401);
    const tokens = session("a1", "a2");
    const stream = new ReadableStream();
    const response = await authenticatedTransport(
      api.transport,
      tokens.value,
    )(request({ body: stream, duplex: "half" }));
    assert.equal(response.status, 401);
    assert.equal(api.sent.length, 1);
    assert.deepEqual(tokens.rejected, []);
  });

  it("leaves an explicit Authorization header and anonymous calls alone", async () => {
    const api = backend(200, 200);
    const tokens = session(null);
    const call = authenticatedTransport(api.transport, tokens.value);
    await call(request({ headers: { Authorization: "Bearer explicit" } }));
    await call(request());
    assert.deepEqual(api.sent[0]?.headers, { Authorization: "Bearer explicit" });
    assert.equal(api.sent[1] && authorization(api.sent[1]), undefined);
    assert.equal(tokens.value.accessToken.mock.callCount(), 1);
  });
});
