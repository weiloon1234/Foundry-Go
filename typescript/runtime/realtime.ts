interface RealtimeEvent { readonly id: string; readonly name: string; readonly direction: string; readonly payload: string; readonly accepted_acknowledgement: boolean }
interface RealtimeChannel { readonly id: string; readonly name: string; readonly room: URLParameter; readonly owned_rooms: boolean; readonly presence?: string; readonly replay: { readonly messages: number; readonly bytes: number }; readonly events: readonly RealtimeEvent[] }
interface RealtimeDescription {
  readonly protocol: { readonly version: number; readonly subprotocol: string; readonly max_room_bytes: number; readonly max_replay_messages: number;
    readonly actions: { readonly subscribe: string; readonly unsubscribe: string; readonly message: string };
    readonly responses: { readonly subscribed: string; readonly unsubscribed: string; readonly acknowledged: string; readonly accepted: string; readonly error: string; readonly event: string; readonly presence_joined: string; readonly presence_left: string; readonly presence_updated: string };
    readonly codes: readonly string[] };
  readonly limits: { readonly subscriptions: number; readonly frame_bytes: number; readonly presence_members: number; readonly member_bytes: number; readonly deduplication_entries: number; readonly operation_ms: number; readonly payload: JSONLimits };
  readonly channels: readonly RealtimeChannel[];
}
/** Supply an already-open connection which negotiated the exported subprotocol. */
export interface RealtimeTransport {
  readonly protocol: string;
  send(text: string): void | Promise<void>;
  listen(receive: (text: string | Uint8Array) => void, closed: () => void): () => void;
  close(): void;
}
export interface RealtimeOptions extends CodecOptions { readonly onError?: (error: Error) => void }
export interface SubscribeOptions { readonly replay?: number; readonly signal?: AbortSignal }
export interface PublishOptions { readonly signal?: AbortSignal; readonly onAccepted?: () => void }
export interface EventInfo { readonly messageID: string; readonly replayed: boolean; readonly room?: string }
export interface PresenceMember<T> { readonly id: string; readonly data: T; readonly connections: number }
export interface PresenceChange<T> { readonly kind: "joined" | "left" | "updated"; readonly member: PresenceMember<T> }
export class RealtimeError extends Error {
  constructor(readonly code: string) { super("Realtime operation failed"); this.name = "RealtimeError"; }
}
interface RoomState {
  readonly channel: RealtimeChannel; readonly room?: string; readonly key: string;
  phase: "idle" | "subscribing" | "subscribed" | "unsubscribing" | "disposed";
  readonly handlers: Map<string, Set<(payload: unknown, info: EventInfo) => void>>;
  readonly presence: Set<(change: PresenceChange<unknown>) => void>;
}
interface PendingOperation {
  readonly state: RoomState; readonly action: string; readonly event?: RealtimeEvent; readonly accepted?: () => void;
  wasAccepted: boolean; readonly resolve: (members: readonly PresenceMember<unknown>[]) => void; readonly reject: (error: Error) => void; readonly cleanup: () => void;
}
function wireInteger(value: unknown): number {
  if (!(value instanceof JSONNumber) || !integerPattern.test(value.text)) reject("", "protocol");
  const number = Number(value.text); if (!Number.isSafeInteger(number)) reject("", "protocol"); return number;
}
function semanticID(value: unknown): value is string { return typeof value === "string" && value.length <= 128 && /^[a-z0-9][a-z0-9_.-]*$/.test(value); }
function createRealtimeEngine(document: RuntimeDocument, transport: RealtimeTransport, options: RealtimeOptions) {
  const description = document.realtime; if (!description || transport.protocol !== description.protocol.subprotocol) reject("", "protocol");
  const { protocol, limits } = description, responses = protocol.responses, actions = protocol.actions, codec = new WireCodec(document.types);
  // A node requires at least one wire byte. This also admits bounded presence
  // snapshots whose several payloads share one larger frame budget.
  const frameLimits: JSONLimits = { ...limits.payload, Bytes: limits.frame_bytes, Depth: runtimePolicy.maxDepth, Nodes: limits.frame_bytes, Steps: limits.frame_bytes * 2, Issues: 1 };
  const states = new Map<string, RoomState>(), pending = new Map<string, PendingOperation>(), seen = new Set<string>();
  let sequence = 0, stopped = false, detach: (() => void) | undefined;
  const notify = (error: Error): void => {
    try { void Promise.resolve(options.onError?.(error)).catch(() => {}); }
    catch { /* Observers cannot retain protocol state or prevent cleanup. */ }
  };
  const invoke = (callback: () => void): void => {
    try { void Promise.resolve(callback()).catch(() => notify(new RealtimeError("callback_failed"))); }
    catch { notify(new RealtimeError("callback_failed")); }
  };
  const deliver = <T>(listeners: ReadonlySet<T> | undefined, callback: (listener: T) => void): void => {
    // Registration changes during a callback apply to subsequent frames. A
    // live Set iterator could repeatedly visit a removed and re-added listener.
    for (const listener of [...(listeners ?? [])]) {
      if (stopped) return;
      if (listeners?.has(listener)) invoke(() => callback(listener));
    }
  };
  const close = (error = new RealtimeError("closed")): void => {
    if (stopped) return; stopped = true;
    try { detach?.(); } catch { /* Continue owned cleanup. */ }
    for (const operation of pending.values()) { operation.cleanup(); operation.reject(error); }
    pending.clear(); seen.clear();
    for (const state of states.values()) { state.phase = "disposed"; state.handlers.clear(); state.presence.clear(); } states.clear();
    try { transport.close(); } catch { /* The client is already stopped. */ }
  };
  const finish = (id: string, operation: PendingOperation, error?: Error, members: readonly PresenceMember<unknown>[] = []): void => {
    pending.delete(id); operation.cleanup();
    if (operation.action === actions.subscribe) operation.state.phase = error ? "idle" : "subscribed";
    if (operation.action === actions.unsubscribe) operation.state.phase = error ? "subscribed" : "idle";
    if (error) operation.reject(error); else operation.resolve(members);
  };
  const request = (state: RoomState, action: string, event?: RealtimeEvent, payload?: unknown, settings: SubscribeOptions & PublishOptions = {}): Promise<readonly PresenceMember<unknown>[]> => {
    checkAbort(settings.signal);
    if (stopped || state.phase === "disposed") return Promise.reject(new RealtimeError("closed"));
    if (pending.size >= limits.subscriptions || sequence === Number.MAX_SAFE_INTEGER) return Promise.reject(new RealtimeError("capacity_exceeded"));
    if (action === actions.subscribe && state.phase !== "idle" || action !== actions.subscribe && state.phase !== "subscribed") return Promise.reject(new RealtimeError("invalid_state"));
    const frame: Record<string, unknown> = { v: new JSONNumber(String(protocol.version)), action, id: "r" + ++sequence, channel: state.channel.id, ...(state.room === undefined ? {} : { room: state.room }) };
    if (event) {
      if (event.direction !== "client_to_server") reject("", "wrong_direction");
      frame.event = event.id; frame.payload = parseWire(codec.encode(event.payload, payload, limits.payload), limits.payload);
      if (settings.onAccepted && !event.accepted_acknowledgement) reject("", "accepted_not_declared");
    }
    if (settings.replay !== undefined) {
      if (action !== actions.subscribe || !Number.isInteger(settings.replay) || settings.replay < 0 || settings.replay > Math.min(state.channel.replay.messages, protocol.max_replay_messages)) reject("", "replay");
      frame.replay = new JSONNumber(String(settings.replay));
    }
    const text = writeWire(frame, frameLimits), id = frame.id as string;
    if (action === actions.subscribe) state.phase = "subscribing"; if (action === actions.unsubscribe) state.phase = "unsubscribing";
    return new Promise((resolve, rejectPromise) => {
      const abort = (): void => { const error = new RealtimeError("aborted"); close(error); };
      const timer = setTimeout(() => { const error = new RealtimeError("operation_timed_out"); notify(error); close(error); }, limits.operation_ms);
      const cleanup = (): void => { clearTimeout(timer); settings.signal?.removeEventListener("abort", abort); };
      pending.set(id, { state, action, ...(event ? { event } : {}), ...(settings.onAccepted ? { accepted: settings.onAccepted } : {}), wasAccepted: false, resolve, reject: rejectPromise, cleanup });
      settings.signal?.addEventListener("abort", abort, { once: true });
      if (settings.signal?.aborted) { abort(); return; }
      try { Promise.resolve(transport.send(text)).catch(() => { const error = new RealtimeError("send_failed"); notify(error); close(error); }); }
      catch { const error = new RealtimeError("send_failed"); notify(error); close(error); }
    });
  };
  const member = (value: unknown, channel: RealtimeChannel, leaving = false): PresenceMember<unknown> => {
    const record = ownInput(value, ["id", "data", "connections"], "");
    if (!channel.presence || typeof record.id !== "string" || !/^[a-f0-9]{64}$/.test(record.id) || !Object.hasOwn(record, "data")) reject("", "presence");
    const connections = wireInteger(record.connections); if (connections < (leaving ? 0 : 1)) reject("", "presence");
    const payloadLimits = { ...limits.payload, Bytes: limits.member_bytes };
    const data = codec.decode(channel.presence, writeWire(record.data, payloadLimits), payloadLimits);
    return immutable({ id: record.id, data, connections });
  };
  const receive = (input: string | Uint8Array): void => {
    if (stopped) return;
    try {
      const frame = object(parseWire(input, frameLimits));
      if (wireInteger(frame.v) !== protocol.version || typeof frame.type !== "string") reject("", "protocol");
      const type = frame.type;
      const common = ["v", "type", "id", "channel", "room"];
      const allowed = type === responses.error ? [...common, "code"] : type === responses.event ? ["v", "type", "channel", "room", "event", "message_id", "payload", "replayed"] : type === responses.subscribed ? [...common, "members"] : [responses.presence_joined, responses.presence_left, responses.presence_updated].includes(type) ? ["v", "type", "channel", "room", "member"] : common;
      ownInput(frame, allowed, "");
      if (frame.room !== undefined && (typeof frame.room !== "string" || !frame.room || textBytes(frame.room) > protocol.max_room_bytes || /[\x00-\x1f\x7f-\x9f]/.test(frame.room))) reject("", "room");
      if (type === responses.error) {
        if (typeof frame.code !== "string" || !protocol.codes.includes(frame.code)) reject("", "protocol");
        const error = new RealtimeError(frame.code);
        if (frame.id === undefined) { notify(error); close(error); return; }
        if (!semanticID(frame.id)) reject("", "protocol"); const operation = pending.get(frame.id);
        if (!operation) reject("", "correlation");
        // Admission errors may precede channel binding. Other errors retain scope.
        if (frame.channel !== undefined && frame.channel !== operation.state.channel.id || frame.room !== undefined && frame.room !== operation.state.room) reject("", "correlation");
        finish(frame.id, operation, error); return;
      }
      if ([responses.subscribed, responses.unsubscribed, responses.acknowledged, responses.accepted].includes(type)) {
        if (!semanticID(frame.id)) reject("", "correlation"); const operation = pending.get(frame.id);
        if (!operation || frame.channel !== operation.state.channel.id || frame.room !== operation.state.room) reject("", "correlation");
        if (type === responses.accepted) {
          if (operation.action !== actions.message || !operation.event?.accepted_acknowledgement || operation.wasAccepted) reject("", "acknowledgement");
          operation.wasAccepted = true; if (operation.accepted) invoke(operation.accepted); return;
        }
        const expected = operation.action === actions.subscribe ? responses.subscribed : operation.action === actions.unsubscribe ? responses.unsubscribed : responses.acknowledged;
        if (type !== expected) reject("", "acknowledgement");
        const members: PresenceMember<unknown>[] = [];
        if (frame.members !== undefined) {
          if (!Array.isArray(frame.members) || frame.members.length > limits.presence_members || !operation.state.channel.presence) reject("", "presence");
          const ids = new Set<string>(); for (const value of frame.members) { const decoded = member(value, operation.state.channel); if (ids.has(decoded.id)) reject("", "presence"); ids.add(decoded.id); members.push(decoded); }
        }
        finish(frame.id, operation, undefined, immutable(members)); return;
      }
      const channel = description.channels.find(channel => channel.id === frame.channel);
      if (!channel) reject("", "channel");
      const targets = [...states.values()].filter(state => state.channel.id === channel.id && (state.phase === "subscribed" || state.phase === "unsubscribing") && (frame.room === undefined || state.room === frame.room));
      if (!targets.length) reject("", "not_subscribed");
      if (type === responses.event) {
        const event = channel.events.find(event => event.id === frame.event);
        if (!event || event.direction !== "server_to_client" || typeof frame.message_id !== "string" || !uuidPattern.test(frame.message_id) || /^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(frame.message_id) || frame.replayed !== undefined && typeof frame.replayed !== "boolean" || !Object.hasOwn(frame, "payload")) reject("", "event");
        const payload = immutable(codec.decode(event.payload, writeWire(frame.payload, limits.payload), limits.payload));
        const messageID = frame.message_id.toLowerCase(); if (seen.has(messageID)) return;
        if (seen.size >= limits.deduplication_entries) seen.delete(seen.values().next().value!); seen.add(messageID);
        const info: EventInfo = Object.freeze({ messageID, replayed: frame.replayed === true, ...(typeof frame.room === "string" ? { room: frame.room } : {}) });
        for (const target of targets) deliver(target.handlers.get(event.name), handler => handler(payload, info));
        return;
      }
      const kind = type === responses.presence_joined ? "joined" : type === responses.presence_left ? "left" : type === responses.presence_updated ? "updated" : undefined;
      if (!kind) reject("", "protocol"); const decoded = member(frame.member, channel, kind === "left");
      const change: PresenceChange<unknown> = Object.freeze({ kind, member: decoded });
      for (const target of targets) if (target.room === frame.room) deliver(target.presence, handler => handler(change));
    } catch (error) { const failure = error instanceof ContractError ? error : new RealtimeError("malformed"); notify(failure); close(new RealtimeError("malformed")); }
  };
  try { detach = transport.listen(receive, () => close(new RealtimeError("closed"))); if (stopped) detach(); }
  catch { close(new RealtimeError("listen_failed")); }
  return {
    close,
    room(name: string, input: unknown) {
      if (stopped) throw new RealtimeError("closed");
      const channel = description.channels.find(channel => channel.name === name); if (!channel) reject("", "channel");
      if (input === undefined && channel.owned_rooms) reject("", "room");
      const room = input === undefined ? undefined : urlValue(codec, channel.room, input, limits.payload, options);
      if (room !== undefined && (!room || textBytes(room) > protocol.max_room_bytes || /[\x00-\x1f\x7f-\x9f]/.test(room))) reject("", "room");
      const key = JSON.stringify([channel.id, room ?? null]);
      let state = states.get(key);
      if (!state) { if (states.size >= limits.subscriptions) throw new RealtimeError("capacity_exceeded"); state = { channel, ...(room === undefined ? {} : { room }), key, phase: "idle", handlers: new Map(), presence: new Set() }; states.set(key, state); }
      const selected = state;
      return {
        subscribe(settings: SubscribeOptions = {}) { return request(selected, actions.subscribe, undefined, undefined, settings); },
        async unsubscribe(settings: CallOptions = {}): Promise<void> { await request(selected, actions.unsubscribe, undefined, undefined, settings); },
        async publish(name: string, payload: unknown, settings: PublishOptions = {}): Promise<void> { const event = channel.events.find(event => event.name === name); if (!event) reject("", "event"); await request(selected, actions.message, event, payload, settings); },
        on(name: string, handler: (payload: unknown, info: EventInfo) => void): () => void {
          if (selected.phase === "disposed") throw new RealtimeError("closed");
          const event = channel.events.find(event => event.name === name); if (!event || event.direction !== "server_to_client") reject("", "event");
          let callbacks = selected.handlers.get(name); if (!callbacks) { callbacks = new Set(); selected.handlers.set(name, callbacks); }
          if (callbacks.size >= limits.subscriptions) throw new RealtimeError("capacity_exceeded"); callbacks.add(handler); return () => { callbacks!.delete(handler); };
        },
        onPresence(handler: (change: PresenceChange<unknown>) => void): () => void { if (!channel.presence) reject("", "presence"); if (selected.phase === "disposed") throw new RealtimeError("closed"); if (selected.presence.size >= limits.subscriptions) throw new RealtimeError("capacity_exceeded"); selected.presence.add(handler); return () => { selected.presence.delete(handler); }; },
        dispose(): void { if (selected.phase !== "idle") throw new RealtimeError("unsubscribe_first"); selected.phase = "disposed"; selected.handlers.clear(); selected.presence.clear(); states.delete(key); },
      };
    },
  };
}
