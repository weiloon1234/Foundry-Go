import * as sdk from "./contracts_foundry.gen.js";

// doc-example:begin realtime-connection
export async function withRealtimeConnection(
  url: string,
  issueTicket: (signal: AbortSignal) => Promise<string>,
  signal: AbortSignal,
  use: (realtime: sdk.Realtime) => Promise<void>,
  onError: (error: Error) => void,
): Promise<void> {
  signal.throwIfAborted();
  const ticket = await issueTicket(signal);
  const transport = await openBrowserSocket(url, ticket, signal);
  let realtime: sdk.Realtime;
  try {
    realtime = sdk.createRealtime(transport, { onError });
  } catch (error) {
    transport.close();
    throw error;
  }
  try {
    await use(realtime);
  } finally {
    realtime.close();
  }
}
// doc-example:end realtime-connection

// doc-example:begin realtime-room
type Updates = ReturnType<sdk.Realtime["channels"]["updates"]>;
type UpdateListener = Parameters<Updates["on"]["updated"]>[0];

export async function withUpdatesRoom(
  realtime: sdk.Realtime,
  roomID: sdk.Int64,
  render: UpdateListener,
  use: (room: Updates) => Promise<void>,
): Promise<void> {
  const room = realtime.channels.updates(roomID);
  const off = room.on.updated(render);
  let subscribed = false;
  try {
    await room.subscribe({ replay: 4 });
    subscribed = true;
    await use(room);
  } finally {
    try {
      if (subscribed) await room.unsubscribe();
    } finally {
      off();
      room.dispose();
    }
  }
}

export async function publishUpdate(
  room: Updates,
  payload: Parameters<Updates["publish"]["relay"]>[0],
  onAccepted: () => void,
): Promise<void> {
  await room.publish.relay(payload, { onAccepted });
}
// doc-example:end realtime-room

// doc-example:begin browser-realtime-transport
async function openBrowserSocket(
  url: string,
  ticket: string,
  signal: AbortSignal,
): Promise<sdk.RealtimeTransport> {
  signal.throwIfAborted();
  const native = new WebSocket(url, sdk.realtimeProtocols(ticket));
  native.binaryType = "arraybuffer";
  await new Promise<void>((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer);
      native.removeEventListener("open", opened);
      native.removeEventListener("error", failed);
      native.removeEventListener("close", failed);
      signal.removeEventListener("abort", aborted);
    };
    const opened = () => {
      cleanup();
      resolve();
    };
    const failed = () => {
      cleanup();
      native.close();
      reject(new Error("Realtime connection did not open"));
    };
    const aborted = () => {
      cleanup();
      native.close();
      reject(signal.reason);
    };
    const timer = setTimeout(failed, 10_000);
    native.addEventListener("open", opened);
    native.addEventListener("error", failed);
    native.addEventListener("close", failed);
    signal.addEventListener("abort", aborted, { once: true });
    if (signal.aborted) aborted();
  });
  return {
    protocol: native.protocol,
    send: (text) => native.send(text),
    close: () => native.close(),
    listen(receive, closed) {
      if (native.readyState !== WebSocket.OPEN) {
        closed();
        return () => {};
      }
      const message = (event: MessageEvent<unknown>) => {
        if (typeof event.data === "string") receive(event.data);
        else if (event.data instanceof ArrayBuffer) receive(new Uint8Array(event.data));
        else closed();
      };
      native.addEventListener("message", message);
      native.addEventListener("close", closed);
      native.addEventListener("error", closed);
      return () => {
        native.removeEventListener("message", message);
        native.removeEventListener("close", closed);
        native.removeEventListener("error", closed);
      };
    },
  };
}
// doc-example:end browser-realtime-transport
