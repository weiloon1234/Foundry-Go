/**
 * Streaming Fetch transport for createClient and createPortalSession.
 * The client owns decoding and byte limits; Fetch owns networking and cancellation.
 * Declared redirects are forwarded, with "error" as the fallback. Requires Fetch
 * and Web Streams support; no network resources are acquired until called.
 */
export const fetchTransport: HTTPTransport = async request => {
  const init: RequestInit & { duplex?: "half" } = {
    method: request.method,
    headers: request.headers,
    ...(request.body === undefined ? {} : { body: request.body }),
    ...(request.signal === undefined ? {} : { signal: request.signal }),
    ...(request.body instanceof ReadableStream ? { duplex: "half" } : {}),
    credentials: request.credentials,
    redirect: request.redirect ?? "error",
  };
  const response = await fetch(request.url, init);
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  async function* chunks(): AsyncIterable<Uint8Array> {
    reader = response.body?.getReader();
    if (!reader) return;
    try {
      for (;;) {
        const next = await reader.read();
        if (next.done) return;
        yield next.value;
      }
    } finally {
      try { await reader.cancel(); }
      finally { reader.releaseLock(); reader = undefined; }
    }
  }
  return {
    status: response.status,
    headers: Object.fromEntries(response.headers),
    body: chunks(),
    close: async () => {
      if (reader) await reader.cancel();
      else if (response.body && !response.body.locked) await response.body.cancel();
    },
  };
};
