/**
 * fetchTransport streams responses for createClient and createPortalSession.
 * the client owns decoding and byte limits; fetch owns networking and cancellation.
 * declared redirects are forwarded, with "error" as the fallback. requires fetch
 * and web streams support; no network resources are acquired until called.
 */
export const fetchTransport: HTTPTransport = async request => {
  const response = await fetch(request.url, {
    method: request.method,
    headers: request.headers,
    ...(request.body === undefined ? {} : { body: request.body }),
    ...(request.signal === undefined ? {} : { signal: request.signal }),
    ...(request.body instanceof ReadableStream ? { duplex: "half" } : {}),
    credentials: request.credentials,
    redirect: request.redirect ?? "error",
  });
  let reader: (NonNullable<typeof response.body>["getReader"] extends () => infer T ? T : never) | undefined;
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
