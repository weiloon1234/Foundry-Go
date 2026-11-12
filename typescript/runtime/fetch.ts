/**
 * fetchTransport streams responses for createClient and createPortalSession.
 * the client owns decoding and byte limits; fetch owns networking and cancellation.
 * declared redirects are forwarded, with "error" as the fallback. requires fetch
 * and web streams support; no network resources are acquired until called.
 */
export const fetchTransport: HTTPTransport = async request => {
  const timeoutMs = requestTimeout(request.requestTimeoutMs);
  const controller = timeoutMs > 0 && (request.body === undefined || typeof request.body === "string")
    ? new AbortController() : undefined;
  const abort = () => controller?.abort(request.signal?.reason);
  if (controller) {
    if (request.signal?.aborted) abort();
    else request.signal?.addEventListener("abort", abort, { once: true });
  }
  const signal = controller?.signal ?? request.signal;
  const failure = (error: unknown): unknown => signal?.aborted ? signal.reason : error;
  const timer = controller ? setTimeout(() => controller.abort(new DOMException(
    request.method.toLowerCase() === "get" || request.method.toLowerCase() === "head"
      ? "Request timed out before response headers"
      : "Request timed out before response headers; outcome is unknown",
    "TimeoutError",
  )), timeoutMs) : undefined;
  const cleanup = () => request.signal?.removeEventListener("abort", abort);
  let response: Response;
  try {
    checkAbort(signal);
    response = await fetch(request.url, {
      method: request.method,
      headers: request.headers,
      ...(request.body === undefined ? {} : { body: request.body }),
      ...(signal === undefined ? {} : { signal }),
      ...(request.body instanceof ReadableStream ? { duplex: "half" } : {}),
      credentials: request.credentials,
      redirect: request.redirect ?? "error",
    });
  } catch (error) {
    cleanup();
    // fetch may reject with AbortError instead of the supplied reason.
    throw failure(error);
  } finally {
    clearTimeout(timer);
  }
  let reader: (NonNullable<typeof response.body>["getReader"] extends () => infer T ? T : never) | undefined;
  async function* chunks(): AsyncIterable<Uint8Array> {
    reader = response.body?.getReader();
    if (!reader) { cleanup(); return; }
    try {
      for (;;) {
        const next = await reader.read();
        if (next.done) return;
        yield next.value;
      }
    } catch (error) {
      throw failure(error);
    } finally {
      try { await reader.cancel(); }
      catch (error) { throw failure(error); }
      finally { reader.releaseLock(); reader = undefined; cleanup(); }
    }
  }
  return {
    status: response.status,
    headers: Object.fromEntries(response.headers),
    body: chunks(),
    close: async () => {
      try {
        if (reader) await reader.cancel();
        else if (response.body && !response.body.locked) await response.body.cancel();
      } catch (error) {
        throw failure(error);
      } finally { cleanup(); }
    },
  };
};
