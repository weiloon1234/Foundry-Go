import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { pathToFileURL } from "node:url";

const generated = await import(pathToFileURL(process.argv[2]).href);
const standalone = await import(pathToFileURL(process.argv[3]).href);
const surface = await import(pathToFileURL(process.argv[4]).href);
assert.equal(surface.fetchTransport, generated.fetchTransport);

const request = {
  method: "POST",
  url: "https://api.example.test/items",
  headers: { "content-type": "text/plain" },
  credentials: "same-origin",
  maxBodyBytes: 1024,
};

for (const [name, { fetchTransport, createClient, ContractError }] of [["generated", generated], ["standalone", standalone]]) {
  describe(`${name} fetchTransport`, () => {
    it("forwards signal, credentials, method, headers and body", async t => {
      const signal = new AbortController().signal;
      const calls = [];
      t.mock.method(globalThis, "fetch", async (url, init) => {
        calls.push({ url, init });
        return new Response(null, { status: 204, headers: { "x-result": "empty" } });
      });
      for (const credentials of ["omit", "same-origin", "include"]) {
        const response = await fetchTransport({ ...request, credentials, signal, body: "payload" });
        assert.equal(response.status, 204);
        assert.equal(response.headers["x-result"], "empty");
        await response.close();
      }
      assert.equal(calls.length, 3);
      for (const [index, { url, init }] of calls.entries()) {
        assert.equal(url, request.url);
        assert.equal(init.method, request.method);
        assert.equal(init.headers, request.headers);
        assert.equal(init.body, "payload");
        assert.notEqual(init.signal, signal);
        assert.equal(init.signal.aborted, false);
        assert.equal(init.credentials, ["omit", "same-origin", "include"][index]);
      }
    });

    for (const method of ["GET", "HEAD", "POST", "DELETE"]) it(`times out stalled ${method} headers with a native TimeoutError`, async t => {
      t.mock.timers.enable({ apis: ["setTimeout"] });
      let sentSignal;
      t.mock.method(globalThis, "fetch", (_url, { signal }) => {
        sentSignal = signal;
        return new Promise((_, reject) => signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true }));
      });
      const pending = fetchTransport({ ...request, method, ...(method === "POST" ? { body: "{}" } : {}) });
      const rejected = assert.rejects(pending, error => {
        assert.ok(error instanceof DOMException);
        assert.equal(error.name, "TimeoutError");
        assert.equal(error, sentSignal.reason);
        assert.equal(error.message.includes("outcome is unknown"), method !== "GET" && method !== "HEAD");
        return true;
      });
      t.mock.timers.tick(29_999);
      assert.equal(sentSignal.aborted, false);
      t.mock.timers.tick(1);
      await rejected;
    });

    it("clears the header timer without limiting body streaming or losing caller cancellation", async t => {
      t.mock.timers.enable({ apis: ["setTimeout"] });
      const caller = new AbortController();
      let sentSignal;
      let writer;
      const streamError = new DOMException("stream aborted by browser", "AbortError");
      const body = new ReadableStream({ start(controller) { writer = controller; } });
      t.mock.method(globalThis, "fetch", async (_url, { signal }) => {
        sentSignal = signal;
        signal.addEventListener("abort", () => writer.error(streamError), { once: true });
        return new Response(body);
      });
      const response = await fetchTransport({ ...request, signal: caller.signal, requestTimeoutMs: 10 });
      t.mock.timers.tick(100);
      assert.equal(sentSignal.aborted, false);
      const iterator = response.body[Symbol.asyncIterator]();
      writer.enqueue(new Uint8Array([9]));
      assert.deepEqual((await iterator.next()).value, new Uint8Array([9]));
      const reason = new DOMException("caller stopped", "AbortError");
      const pending = iterator.next();
      caller.abort(reason);
      assert.equal(sentSignal.reason, reason);
      await assert.rejects(pending, error => error === reason);
      assert.equal(body.locked, false);
      await assert.rejects(response.close(), error => error === reason);
    });

    it("preserves the caller's reason when closing an aborted active reader", async t => {
      const caller = new AbortController();
      let writer;
      const body = new ReadableStream({ start(controller) {
        writer = controller;
        controller.enqueue(new Uint8Array([9]));
      } });
      t.mock.method(globalThis, "fetch", async (_url, { signal }) => {
        signal.addEventListener("abort", () => writer.error(new DOMException("browser abort", "AbortError")), { once: true });
        return new Response(body);
      });
      const response = await fetchTransport({ ...request, signal: caller.signal });
      const iterator = response.body[Symbol.asyncIterator]();
      await iterator.next();
      const reason = new Error("caller cancellation");
      caller.abort(reason);
      await assert.rejects(response.close(), error => error === reason);
      await assert.rejects(iterator.return(), error => error === reason);
      assert.equal(body.locked, false);
    });

    it("preserves caller abort identity before headers and skips already aborted requests", async t => {
      t.mock.timers.enable({ apis: ["setTimeout"] });
      const controller = new AbortController();
      const fetchCall = t.mock.method(globalThis, "fetch", (_url, { signal }) => new Promise((_, reject) => {
        signal.addEventListener("abort", () => reject(signal.reason), { once: true });
      }));
      const pending = fetchTransport({ ...request, signal: controller.signal, requestTimeoutMs: 10 });
      const reason = new Error("caller cancellation");
      controller.abort(reason);
      await assert.rejects(pending, error => error === reason);
      t.mock.timers.tick(100);
      await assert.rejects(fetchTransport({ ...request, signal: controller.signal }), error => error === reason);
      assert.equal(fetchCall.mock.callCount(), 1);
    });

    for (const kind of ["disabled", "blob", "stream"]) it(`does not time out ${kind} requests`, async t => {
      t.mock.timers.enable({ apis: ["setTimeout"] });
      const caller = new AbortController();
      const finish = Promise.withResolvers();
      t.mock.method(globalThis, "fetch", (_url, { signal }) => {
        assert.equal(signal, caller.signal);
        return finish.promise;
      });
      const body = kind === "blob" ? new Blob(["upload"]) : kind === "stream" ? new ReadableStream() : "text";
      let settled = false;
      const pending = fetchTransport({ ...request, body, signal: caller.signal, requestTimeoutMs: kind === "disabled" ? 0 : 10 });
      pending.finally(() => { settled = true; });
      t.mock.timers.tick(60_000);
      await Promise.resolve();
      assert.equal(settled, false);
      finish.resolve(new Response(null, { status: 204 }));
      await (await pending).close();
    });

    it("passes client timeout options to Fetch and rejects invalid limits", async t => {
      t.mock.timers.enable({ apis: ["setTimeout"] });
      t.mock.method(globalThis, "fetch", (_url, { signal }) => new Promise((_, reject) => {
        signal.addEventListener("abort", () => reject(signal.reason), { once: true });
      }));
      const api = createClient(fetchTransport, { requestTimeoutMs: 7 });
      const rejected = assert.rejects(api.browserLogout({}), error => error instanceof DOMException && error.name === "TimeoutError");
      t.mock.timers.tick(7);
      await rejected;
      for (const requestTimeoutMs of [-1, 0.5, NaN, Infinity, 2_147_483_648]) {
        assert.throws(() => createClient(fetchTransport, { requestTimeoutMs }), TypeError);
        await assert.rejects(fetchTransport({ ...request, requestTimeoutMs }), TypeError);
      }
    });

    it("defaults to redirect:error and forwards declared redirect modes", async t => {
      const calls = [];
      t.mock.method(globalThis, "fetch", async (_url, init) => {
        calls.push(init);
        return new Response(null, { status: 204 });
      });
      await (await fetchTransport(request)).close();
      for (const redirect of ["follow", "manual"]) {
        await (await fetchTransport({ ...request, redirect })).close();
      }
      assert.deepEqual(calls.map(init => init.redirect), ["error", "follow", "manual"]);
      assert.ok(calls[0].signal instanceof AbortSignal);
      assert.equal(Object.hasOwn(calls[0], "body"), false);
    });

    it("rejects a browser opaque redirect with opaque_redirect", async t => {
      const fetchCall = t.mock.method(globalThis, "fetch", async (_url, init) => {
        assert.equal(init.redirect, "manual");
        return { status: 0, type: "opaqueredirect", headers: new Headers(), body: null };
      });
      const api = createClient(fetchTransport, { baseURL: "https://api.example.test" });
      await assert.rejects(api.sessionContinue({}), error =>
        error instanceof ContractError && error.issues.some(issue => issue.code === "opaque_redirect"));
      assert.equal(fetchCall.mock.callCount(), 1);
    });

    it("sets duplex:half only for stream bodies, even without a duplex hint", async t => {
      const calls = [];
      t.mock.method(globalThis, "fetch", async (_url, init) => {
        calls.push(init);
        return new Response(null, { status: 204 });
      });
      const stream = new ReadableStream({ start(controller) { controller.close(); } });
      for (const body of [undefined, "text", new Blob(["blob"]), stream]) {
        const response = await fetchTransport({ ...request, ...(body === undefined ? {} : { body }) });
        await response.close();
        const init = calls.at(-1);
        assert.equal(init.body, body);
        assert.equal(Object.hasOwn(init, "duplex"), body === stream);
        if (body === stream) assert.equal(init.duplex, "half");
      }
      await (await fetchTransport({ ...request, body: "text", duplex: "half" })).close();
      assert.equal(Object.hasOwn(calls.at(-1), "duplex"), false);
    });

    it("streams response chunks lazily and cancels and releases on early return", async t => {
      let cancelled = 0;
      const chunk = new Uint8Array([1, 2]);
      const body = new ReadableStream({
        start(controller) { controller.enqueue(chunk); },
        cancel() { cancelled++; },
      });
      t.mock.method(globalThis, "fetch", async () => new Response(body, { status: 202, headers: { "x-stream": "yes" } }));
      const response = await fetchTransport(request);
      assert.equal(response.status, 202);
      assert.equal(response.headers["x-stream"], "yes");
      assert.equal(body.locked, false);
      for await (const next of response.body) {
        assert.equal(next, chunk);
        assert.equal(body.locked, true);
        assert.equal(cancelled, 0);
        break;
      }
      assert.equal(cancelled, 1);
      assert.equal(body.locked, false);
      await response.close();
      await response.close();
      assert.equal(cancelled, 1);
    });

    it("closes an unread response by cancelling its unlocked body", async t => {
      let cancelled = 0;
      const body = new ReadableStream({ cancel() { cancelled++; } });
      t.mock.method(globalThis, "fetch", async () => new Response(body));
      const response = await fetchTransport(request);
      await response.close();
      await response.close();
      assert.equal(cancelled, 1);
      assert.equal(body.locked, false);
      const chunks = [];
      for await (const chunk of response.body) chunks.push(chunk);
      assert.deepEqual(chunks, []);
      assert.equal(body.locked, false);
    });

    it("closes an active reader, unblocks its read and releases on iterator exit", async t => {
      let cancelled = 0;
      const body = new ReadableStream({
        start(controller) { controller.enqueue(new Uint8Array([3])); },
        cancel() { cancelled++; },
      });
      t.mock.method(globalThis, "fetch", async () => new Response(body));
      const response = await fetchTransport(request);
      const iterator = response.body[Symbol.asyncIterator]();
      assert.deepEqual((await iterator.next()).value, new Uint8Array([3]));
      const pending = iterator.next();
      await response.close();
      assert.deepEqual(await pending, { value: undefined, done: true });
      assert.equal(cancelled, 1);
      assert.equal(body.locked, false);
    });

    it("retains a paused reader's lock until its iterator exits after close", async t => {
      let cancelled = 0;
      const body = new ReadableStream({
        start(controller) { controller.enqueue(new Uint8Array([7])); },
        cancel() { cancelled++; },
      });
      t.mock.method(globalThis, "fetch", async () => new Response(body));
      const response = await fetchTransport(request);
      const iterator = response.body[Symbol.asyncIterator]();
      await iterator.next();
      await response.close();
      assert.equal(cancelled, 1);
      assert.equal(body.locked, true);
      await iterator.return();
      assert.equal(body.locked, false);
      assert.equal(cancelled, 1);
    });

    it("releases its reader when the response stream ends normally", async t => {
      const body = new ReadableStream({
        start(controller) {
          controller.enqueue(new Uint8Array([4]));
          controller.enqueue(new Uint8Array([5]));
          controller.close();
        },
      });
      t.mock.method(globalThis, "fetch", async () => new Response(body));
      const response = await fetchTransport(request);
      const bytes = [];
      for await (const chunk of response.body) bytes.push(...chunk);
      assert.deepEqual(bytes, [4, 5]);
      assert.equal(body.locked, false);
      await response.close();
    });

    it("releases its reader even when cancellation fails", async t => {
      const failure = new Error("cancel failed");
      const body = new ReadableStream({
        start(controller) { controller.enqueue(new Uint8Array([6])); },
        cancel() { throw failure; },
      });
      t.mock.method(globalThis, "fetch", async () => new Response(body));
      const response = await fetchTransport(request);
      await assert.rejects(async () => {
        for await (const _chunk of response.body) break;
      }, error => error === failure);
      assert.equal(body.locked, false);
    });

    it("handles a response without a body", async t => {
      t.mock.method(globalThis, "fetch", async () => new Response(null, { status: 204 }));
      const response = await fetchTransport(request);
      const chunks = [];
      for await (const chunk of response.body) chunks.push(chunk);
      assert.deepEqual(chunks, []);
      await response.close();
      await response.close();
    });

    it("preserves Fetch rejection identity", async t => {
      const failure = new TypeError("network failed");
      t.mock.method(globalThis, "fetch", async () => { throw failure; });
      await assert.rejects(fetchTransport(request), error => error === failure);
    });
  });
}
