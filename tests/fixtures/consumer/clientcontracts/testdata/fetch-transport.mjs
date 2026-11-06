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

for (const [name, { fetchTransport }] of [["generated", generated], ["standalone", standalone]]) {
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
        assert.equal(init.signal, signal);
        assert.equal(init.credentials, ["omit", "same-origin", "include"][index]);
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
      assert.equal(Object.hasOwn(calls[0], "signal"), false);
      assert.equal(Object.hasOwn(calls[0], "body"), false);
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
