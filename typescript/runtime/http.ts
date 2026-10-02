interface URLParameter { readonly presentation?: Presentation; readonly name: string; readonly type: string; readonly syntax: string; readonly required: boolean; readonly repeated: boolean; readonly catch_all?: boolean; readonly default_url?: string }
interface MultipartPart extends URLParameter { readonly kind: string }
interface Payload { readonly type?: string; readonly media_type?: string; readonly fields?: readonly URLParameter[]; readonly parts?: readonly MultipartPart[]; readonly file?: { readonly media_types: readonly string[]; readonly seekable: boolean }; readonly raw?: { readonly media_types: readonly string[] } }
interface SignedURLPolicy { readonly version: string; readonly algorithm: string; readonly expires_parameter: string; readonly signature_parameter: string; readonly relative: boolean; readonly permanent?: boolean; readonly ignored_parameters?: readonly string[] }
interface IdempotencyPolicy { readonly operation: string; readonly version: number; readonly header: string; readonly key_pattern: string; readonly max_key_bytes: number; readonly min_key_bytes: number; readonly fingerprint: string; readonly success_only: boolean; readonly duplicate_wait_ms: number; readonly retention_seconds: number; readonly application_headers: readonly string[] }
interface Operation {
  readonly name: string; readonly route: { readonly id: string; readonly method: string; readonly path: string; readonly access: "public" | "guarded"; readonly parameters: readonly string[]; readonly middlewares?: readonly string[]; readonly documentation?: { readonly summary?: string; readonly description?: string; readonly tags?: readonly string[]; readonly deprecated?: boolean }; readonly authentication?: { readonly guard: string; readonly provider: string; readonly optional: boolean; readonly required_scopes?: readonly string[]; readonly required_permissions?: readonly string[]; readonly credential: { readonly source: string; readonly kind: string; readonly name: string; readonly origin_protection?: boolean } }; readonly signed_url?: SignedURLPolicy };
  readonly path: readonly URLParameter[]; readonly query: readonly URLParameter[]; readonly body?: Payload; readonly response?: Payload;
  readonly idempotency?: IdempotencyPolicy; readonly status: number; readonly statuses?: readonly number[]; readonly redirect?: boolean; readonly file_transfer_bytes?: number; readonly preparation?: boolean; readonly validation?: RuleDescription; readonly errors: readonly string[];
  readonly limits: { readonly Body: JSONLimits; readonly Response: JSONLimits; readonly Query: { readonly Bytes: number; readonly Pairs: number; readonly Issues: number }; readonly Form: { readonly Bytes: number; readonly Pairs: number; readonly Issues: number }; readonly Validation: ValidationLimits;
    readonly Multipart: { readonly Bytes: number; readonly FileBytes: number; readonly Parts: number; readonly Files: number; readonly Readers: number; readonly HeaderBytes: number; readonly FieldBytes: number; readonly FieldsBytes: number; readonly Issues: number };
    readonly Files: { readonly Bytes: number; readonly Ranges: number; readonly RangeBytes: number }; readonly Raw?: { readonly Bytes: number } };
}
interface RealtimeEvent { readonly id: string; readonly name: string; readonly direction: string; readonly payload: string; readonly accepted_acknowledgement: boolean }
interface RealtimeChannel { readonly id: string; readonly name: string; readonly room: URLParameter; readonly owned_rooms: boolean; readonly presence?: string; readonly replay: { readonly messages: number; readonly bytes: number }; readonly events: readonly RealtimeEvent[] }
interface RealtimeDescription {
  readonly protocol: { readonly version: number; readonly subprotocol: string; readonly ticket_subprotocol_prefix: string; readonly max_room_bytes: number; readonly max_replay_messages: number;
    readonly actions: { readonly subscribe: string; readonly unsubscribe: string; readonly message: string };
    readonly responses: { readonly subscribed: string; readonly unsubscribed: string; readonly acknowledged: string; readonly accepted: string; readonly error: string; readonly event: string; readonly presence_joined: string; readonly presence_left: string; readonly presence_updated: string };
    readonly codes: readonly string[] };
  readonly limits: { readonly subscriptions: number; readonly frame_bytes: number; readonly presence_members: number; readonly member_bytes: number; readonly deduplication_entries: number; readonly operation_ms: number; readonly inbound_queue: number; readonly message_rate: { readonly requests: number; readonly window_ms: number }; readonly payload: JSONLimits };
  readonly channels: readonly RealtimeChannel[];
}
interface RuntimeDocument {
  readonly version: number; readonly types: readonly WireType[]; readonly http: readonly Operation[]; readonly error_type: string;
  readonly locales?: { readonly messages: readonly { readonly key: string; readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" }[] };
  readonly errors: readonly { readonly error_code: string; readonly status: number; readonly message: string }[]; readonly realtime?: RealtimeDescription;
}
declare const idempotencyKeyBrand: unique symbol;
export type IdempotencyKey = string & { readonly [idempotencyKeyBrand]: true };
/** Validate a stable submission key. Generate it once outside retry loops. */
export function idempotencyKey(value: string): IdempotencyKey {
  if (typeof value !== "string" || new RegExp(idempotencyKeyPattern).exec(value)?.[0] !== value) reject("/idempotencyKey", "idempotency_key");
  return value as IdempotencyKey;
}
export type IdempotencyErrorCode = "idempotency_bad_key" | "idempotency_mismatch" | "idempotency_in_progress" | "idempotency_capacity" | "idempotency_unavailable";
export interface CallOptions { readonly signal?: AbortSignal; readonly headers?: Readonly<Record<string, string>> }
export interface Upload { readonly data: Blob; readonly filename: string }
/** Raw request body data; a stream is bounded while it is sent. */
export type RawData = Blob | ArrayBuffer | ArrayBufferView | ReadableStream<Uint8Array>;
/**
 * A transport forwards every field to its native client. redirect is "manual" for
 * redirect operations, whose Location is returned rather than followed; duplex
 * is "half" for a streamed request body (required by fetch for ReadableStream).
 */
export interface HTTPRequest {
  readonly method: string; readonly url: string; readonly headers: Readonly<Record<string, string>>;
  readonly body?: string | Blob | ReadableStream<Uint8Array>; readonly signal?: AbortSignal; readonly credentials: RequestCredentials;
  readonly redirect: "follow" | "manual"; readonly duplex?: "half";
  /** Includes the framework-owned multipart framing. */
  readonly maxBodyBytes: number;
}
export type ResponseBody = string | Uint8Array | AsyncIterable<Uint8Array>;
export interface HTTPResponse { readonly status: number; readonly headers: Readonly<Record<string, string>>; readonly body: ResponseBody; close(): void | Promise<void> }
export type HTTPTransport = (request: HTTPRequest) => Promise<HTTPResponse>;
/**
 * Responses decode tolerantly by default: unknown properties are ignored and unknown
 * enum values become UnknownEnumValue, so additive server changes reach deployed
 * clients. Set strictResponses to reject them. Requests are always strict.
 */
export interface ClientOptions extends CodecOptions { readonly validationMessages?: ValidationMessages; readonly baseURL?: string; readonly credentials?: RequestCredentials; readonly headers?: Readonly<Record<string, string>>; readonly strictResponses?: boolean }
export interface FileResult { readonly status: number; readonly headers: Readonly<Record<string, string>>; readonly body: AsyncIterable<Uint8Array>; close(): Promise<void> }
/** A response whose operation declares several success statuses. */
export interface StatusResult<S extends number, T> { readonly status: S; readonly body: T }
/** A redirect operation's relative target on the server's origin; it is not followed. */
export interface RedirectResult<S extends number> { readonly status: S; readonly location: string }
/** One server-sent event. id is the last event ID the stream set; name defaults to "message". */
export interface ServerEvent<T> { readonly id?: string; readonly name: string; readonly data: T; readonly retry?: number }
/**
 * A typed server-sent event stream. Iterate it once; the response closes when
 * iteration ends. Call close() to stop early. Pass Last-Event-ID in call headers
 * to resume after reconnecting.
 */
export interface EventStreamResult<T> extends AsyncIterable<ServerEvent<T>> { readonly status: number; readonly headers: Readonly<Record<string, string>>; close(): Promise<void> }
export class APIError<T = unknown> extends Error {
  constructor(readonly status: number, readonly code: string, readonly response: T | undefined, readonly retryAfterSeconds?: number) { super("API request failed"); this.name = "APIError"; }
}
/**
 * The server answered, but its response broke this operation's contract: its
 * status, headers, error envelope or body. The request may already have taken
 * effect, so reconcile before retrying. status is the HTTP status received, or
 * 0 when the transport reported no usable status.
 */
export class ResponseContractError extends ContractError {
  constructor(readonly status: number, issues: readonly Issue[]) { super(issues); this.name = "ResponseContractError"; }
}
// Contract failures after the transport returned belong to the response. The
// status may be the very value just rejected, so keep only an integer one.
function responseFailure(status: unknown, error: unknown): unknown {
  const received = typeof status === "number" && Number.isInteger(status) ? status : 0;
  return error instanceof ContractError && !(error instanceof ResponseContractError) ? new ResponseContractError(received, error.issues) : error;
}
// Failures the invoker raised before calling its transport: only these prove a
// request never left this client. The set is private, so an error thrown by
// application code (a wrapped client or transport) can never claim it.
const unsentFailures = /* @__PURE__ */ new WeakSet<object>();
function beforeSending<T>(run: () => T): T {
  try { return run(); }
  catch (error) { if (error !== null && typeof error === "object") unsentFailures.add(error); throw error; }
}
function checkAbort(signal?: AbortSignal): void { if (signal?.aborted) throw signal.reason ?? new DOMException("Request aborted", "AbortError"); }
function mergeHeaders(...sources: readonly (Readonly<Record<string, string>> | undefined)[]): Record<string, string> {
  const result: Record<string, string> = Object.create(null);
  for (const source of sources) if (source) for (const [name, value] of Object.entries(object(source))) {
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name) || typeof value !== "string" || /[\x00-\x1f\x7f]/.test(value) || !validUnicode(value)) reject("", "header");
    result[name.toLowerCase()] = value;
  }
  return result;
}
function mediaType(value: string | undefined): string { return (value ?? "").split(";", 1)[0]!.trim().toLowerCase(); }
function requireJSON(headers: Readonly<Record<string, string>>): void { if (mediaType(headers["content-type"]) !== "application/json") reject("", "content_type"); }
async function* boundedBody(body: ResponseBody, max: number, signal?: AbortSignal): AsyncIterable<Uint8Array> {
  checkAbort(signal); let count = 0;
  const source = typeof body === "string" ? [encoder.encode(rawText(body, max))] : body instanceof Uint8Array ? [body] : body;
  for await (const chunk of source) {
    checkAbort(signal); if (!(chunk instanceof Uint8Array) || chunk.byteLength > max - count) reject("", "limit");
    count += chunk.byteLength; yield chunk;
  }
  checkAbort(signal);
}
async function collectBody(body: ResponseBody, max: number, signal?: AbortSignal): Promise<Uint8Array> {
  const chunks: Uint8Array[] = []; let count = 0;
  for await (const chunk of boundedBody(body, max, signal)) { chunks.push(chunk.slice()); count += chunk.byteLength; }
  const result = new Uint8Array(count); let at = 0; for (const chunk of chunks) { result.set(chunk, at); at += chunk.byteLength; } return result;
}
function ownInput(value: unknown, allowed: readonly string[], path: string): Record<string, unknown> {
  const record = object(value, path); for (const key of Object.keys(record)) if (!allowed.includes(key)) reject(pointer(path, key), "unknown"); return record;
}
function escapedPath(value: string): string {
  if (value === "" || value === "." || value === ".." || value === "/" || /[\x00-\x1f\x7f]/.test(value) || !validUnicode(value)) reject("", "path");
  return encodeURIComponent(value).replace(/[!'()*]/g, c => "%" + c.charCodeAt(0).toString(16).toUpperCase());
}
function urlValue(codec: WireCodec, parameter: URLParameter, value: unknown, limits: JSONLimits, options: CodecOptions): string {
  const wire = codec.encode(parameter.type, value, limits);
  if (parameter.syntax === "custom") {
    const supplied = options.urlCodecs?.[parameter.type]; if (!supplied) reject("", "missing_url_codec");
    const text = supplied.format(value); if (typeof text !== "string" || !validUnicode(text)) reject(); return text;
  }
  const decoded = parseWire(wire, limits); let text: string;
  if (typeof decoded === "string") text = decoded;
  else if (decoded instanceof JSONNumber) text = decoded.text;
  else if (typeof decoded === "boolean") text = String(decoded);
  else reject("", "url");
  if (parameter.syntax === "model_id" && text.toLowerCase() === "00000000-0000-0000-0000-000000000000") reject("", "url");
  return text;
}
// URL scalars have no nesting; JSON escaping is an internal bridge only.
function urlLimits(budget: { readonly Bytes: number; readonly Issues: number }): JSONLimits {
  return { Bytes: Math.min(Number.MAX_SAFE_INTEGER, budget.Bytes * 6 + 2), Depth: 0, Nodes: 1, Steps: 8, Issues: budget.Issues };
}
function queryValues(codec: WireCodec, parameters: readonly URLParameter[], value: unknown, limits: JSONLimits, options: CodecOptions, budget: { readonly Bytes: number; readonly Pairs: number }, source = "/query"): { pairs: [string, string][]; values: Record<string, unknown> } {
  const input = ownInput(value ?? {}, parameters.map(p => p.name), source), pairs: [string, string][] = [], values: Record<string, unknown> = Object.create(null);
  let wireBytes = 0;
  for (const parameter of parameters) {
    if (!Object.hasOwn(input, parameter.name)) {
      if (parameter.required) reject(pointer(source, parameter.name), "required");
      if (parameter.default_url !== undefined) {
        // The server owns custom-codec defaults; their native logical value is unavailable.
        if (parameter.syntax !== "custom") {
          const type = codec.type(parameter.type), wire = type.kind === "string" ? JSON.stringify(parameter.default_url) : parameter.default_url;
          const defaults = { ...limits, Bytes: Math.max(limits.Bytes, textBytes(wire)) };
          values[parameter.name] = codec.semantic(parameter.type, codec.decode(parameter.type, wire, defaults), defaults);
        }
      }
      continue;
    }
    const value = input[parameter.name], items = parameter.repeated ? value : [value];
    if (!Array.isArray(items) || parameter.required && items.length === 0) reject(pointer(source, parameter.name), "required");
    if (items.length > budget.Pairs - pairs.length) reject(source, "limit");
    for (const item of items) {
      const text = urlValue(codec, parameter, item, limits, options);
      // Bound each scalar before percent-encoding, then count the actual wire.
      if (textBytes(text) > budget.Bytes) reject(source, "limit");
      const pair: [string, string] = [parameter.name, text];
      wireBytes += new URLSearchParams([pair]).toString().length + (pairs.length ? 1 : 0);
      if (wireBytes > budget.Bytes) reject(source, "limit");
      pairs.push(pair);
    }
    values[parameter.name] = parameter.repeated ? items.map(item => codec.semantic(parameter.type, item, limits)) : codec.semantic(parameter.type, value, limits);
  }
  return { pairs, values };
}
function multipartBody(codec: WireCodec, payload: Payload, value: unknown, operation: Operation, options: CodecOptions): Blob {
  const parts = payload.parts ?? [], input = ownInput(value, parts.map(p => p.name), "/body"), chunks: BlobPart[] = [];
  const random = new Uint8Array(24); crypto.getRandomValues(random); const boundary = "foundry-" + [...random].map(byte => byte.toString(16).padStart(2, "0")).join("");
  const limits = operation.limits.Multipart; let count = 0, files = 0, fields = 0, bytes = 0;
  const quoted = (text: string): string => '"' + text.replace(/\\/g, "\\\\").replace(/"/g, '\\"') + '"';
  const append = (name: string, data: Blob | string, media: string, filename?: string): void => {
    if (/[\x00-\x1f\x7f]/.test(name + media + (filename ?? ""))) reject("/body", "header");
    const header = "Content-Disposition: form-data; name=" + quoted(name) + (filename === undefined ? "" : "; filename=" + quoted(filename)) + "\r\nContent-Type: " + media + "\r\n\r\n";
    if (textBytes(header) > limits.HeaderBytes) reject("/body", "limit");
    const opening = "--" + boundary + "\r\n" + header; bytes += textBytes(opening) + (typeof data === "string" ? textBytes(data) : data.size) + 2;
    if (bytes > limits.Bytes) reject("/body", "limit"); chunks.push(opening, data, "\r\n");
  };
  for (const part of parts) {
    if (!Object.hasOwn(input, part.name)) { if (part.required) reject(pointer("/body", part.name), "required"); continue; }
    const items = part.repeated ? input[part.name] : [input[part.name]];
    if (!Array.isArray(items) || part.required && items.length === 0) reject(pointer("/body", part.name), "required");
    for (const item of items) {
      if (++count > limits.Parts) reject("/body", "limit");
      if (part.kind === "file") {
        const file = ownInput(item, ["data", "filename"], pointer("/body", part.name));
        if (!(file.data instanceof Blob) || typeof file.filename !== "string" || !file.filename || !validUnicode(file.filename) || /[\x00-\x1f\x7f/\\]/.test(file.filename)) reject(pointer("/body", part.name), "file");
        if (++files > limits.Files || file.data.size > limits.FileBytes) reject("/body", "limit");
        append(part.name, file.data, file.data.type || "application/octet-stream", file.filename);
      } else {
        const text = part.kind === "json" ? codec.encode(part.type, item, operation.limits.Body) : urlValue(codec, part, item, operation.limits.Body, options);
        const size = textBytes(text); fields += size;
        if (size > limits.FieldBytes || fields > limits.FieldsBytes) reject("/body", "limit");
        append(part.name, text, part.kind === "json" ? "application/json" : "text/plain; charset=utf-8");
      }
      if (bytes > limits.Bytes) reject("/body", "limit");
    }
  }
  const end = "--" + boundary + "--\r\n"; if (bytes + textBytes(end) > limits.Bytes) reject("/body", "limit"); chunks.push(end);
  return new Blob(chunks, { type: "multipart/form-data; boundary=" + boundary });
}
function rawBody(payload: Payload, value: unknown, max: number): { body: Blob | ReadableStream<Uint8Array>; media: string; stream: boolean } {
  const declared = payload.raw!.media_types, input = ownInput(value, ["data", "mediaType"], "/body");
  const media = input.mediaType ?? (declared.length === 1 ? declared[0] : undefined);
  if (typeof media !== "string" || !declared.includes(media)) reject("/body/mediaType", "media_type");
  const data = input.data;
  if (data instanceof Blob) { if (data.size > max) reject("/body", "limit"); return { body: data, media, stream: false }; }
  if (data instanceof ArrayBuffer || ArrayBuffer.isView(data)) {
    const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
    if (bytes.byteLength > max) reject("/body", "limit");
    return { body: new Blob([bytes.slice()]), media, stream: false };
  }
  if (data instanceof ReadableStream) {
    // The server bound is enforced again as the stream is sent.
    let count = 0;
    const bounded = data.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({ transform(chunk, controller) {
      if (!(chunk instanceof Uint8Array) || chunk.byteLength > max - count) { controller.error(new ContractError([{ path: "/body", code: "limit" }])); return; }
      count += chunk.byteLength; controller.enqueue(chunk);
    } }));
    return { body: bounded, media, stream: true };
  }
  reject("/body/data", "raw");
}
// Parses text/event-stream framing: CR, LF or CRLF line ends, comments, and the
// data/event/id/retry fields. Each event's data text is bounded before decoding;
// an unterminated final event is discarded, as in EventSource.
async function* serverEvents(body: ResponseBody, decode: (data: string) => unknown, max: number, signal?: AbortSignal): AsyncIterable<ServerEvent<unknown>> {
  const text = new TextDecoder("utf-8", { fatal: true });
  const source = typeof body === "string" ? [encoder.encode(body)] : body instanceof Uint8Array ? [body] : body;
  let buffer = "", data: string[] = [], size = 0, name = "", id: string | undefined, retry: number | undefined, first = true;
  const line = (value: string): ServerEvent<unknown> | undefined => {
    if (value === "") {
      if (!data.length) { name = ""; return undefined; }
      const event = Object.freeze({ ...(id === undefined ? {} : { id }), name: name || "message", data: decode(data.join("\n")), ...(retry === undefined ? {} : { retry }) });
      data = []; size = 0; name = ""; return event;
    }
    if (value.startsWith(":")) return undefined;
    const colon = value.indexOf(":"), field = colon < 0 ? value : value.slice(0, colon);
    let content = colon < 0 ? "" : value.slice(colon + 1); if (content.startsWith(" ")) content = content.slice(1);
    if (field === "data") { size += textBytes(content) + 1; if (size > max) reject("", "limit"); data.push(content); }
    else if (field === "event") name = content;
    else if (field === "id") { if (!content.includes("\0")) id = content; }
    else if (field === "retry") { if (/^[0-9]{1,9}$/.test(content)) retry = Number(content); }
    return undefined;
  };
  for await (const chunk of source) {
    checkAbort(signal); if (!(chunk instanceof Uint8Array)) reject("", "event_stream");
    try { buffer += text.decode(chunk, { stream: true }); } catch { reject("", "event_stream"); }
    if (first && buffer.length) { if (buffer.startsWith("\ufeff")) buffer = buffer.slice(1); first = false; }
    for (;;) {
      const cr = buffer.indexOf("\r"), lf = buffer.indexOf("\n"), end = cr < 0 ? lf : lf < 0 ? cr : Math.min(cr, lf);
      if (end < 0) break;
      if (buffer[end] === "\r" && end + 1 === buffer.length) break; // A following LF may be in the next chunk.
      const next = buffer[end] === "\r" && buffer[end + 1] === "\n" ? end + 2 : end + 1;
      const event = line(buffer.slice(0, end)); buffer = buffer.slice(next);
      if (event) yield event;
    }
    // A UTF-16 length never exceeds the UTF-8 size, so this cheap check bounds
    // an unterminated line without re-encoding the buffer for every chunk.
    if (buffer.length > max + 64) reject("", "limit");
  }
  checkAbort(signal);
}
function redirectLocation(value: string | undefined): string {
  // Only relative targets on the same origin are accepted, as the server emits.
  if (typeof value !== "string" || !value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\") || /[\x00-\x20\x7f-\uffff\\]/.test(value) || value.length > 8192) reject("", "location");
  return value;
}
function createHTTPInvoker(document: RuntimeDocument, transport: HTTPTransport, options: ClientOptions) {
  const codec = new WireCodec(document.types), operations = new Map(document.http.map(operation => [operation.name, operation])), tolerant = options.strictResponses !== true;
  let base = options.baseURL ?? "";
  if (base) { const parsed = new URL(base); if (!/^https?:$/.test(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash) reject("", "base_url"); base = parsed.href.replace(/\/$/, ""); }
  const prepare = (name: string, request: unknown, call: CallOptions) => {
    checkAbort(call.signal); const operation = operations.get(name); if (!operation) reject("", "operation");
    const input = ownInput(request, ["path", "query", "body", ...(operation.route.signed_url ? ["signedURL"] : []), ...(operation.idempotency ? ["idempotencyKey"] : [])], ""), path = ownInput(input.path ?? {}, operation.path.map(p => p.name), "/path");
    let url = operation.route.path.split("/").map(segment => segment === "" || segment.startsWith("{") ? segment : escapedPath(segment)).join("/");
    for (const parameter of operation.path) {
      if (!Object.hasOwn(path, parameter.name)) reject(pointer("/path", parameter.name), "required");
      const value = urlValue(codec, parameter, path[parameter.name], operation.limits.Body, options);
      const encoded = parameter.catch_all ? value === "" ? "" : value.split("/").map(escapedPath).join("/") : escapedPath(value);
      url = url.replace("{" + parameter.name + (parameter.catch_all ? "..." : "") + "}", encoded);
    }
    const query = queryValues(codec, operation.query, input.query, urlLimits(operation.limits.Query), options, operation.limits.Query), search = new URLSearchParams(query.pairs).toString();
    if (query.pairs.length > operation.limits.Query.Pairs || textBytes(search) > operation.limits.Query.Bytes) reject("/query", "limit");
    const headers = mergeHeaders(options.headers, call.headers);
    if (operation.idempotency) {
      const policy = operation.idempotency, name = policy.header.toLowerCase();
      if (headers[name] !== undefined) reject("/idempotencyKey", "idempotency_header_owned_by_client");
      if (typeof input.idempotencyKey !== "string" || new RegExp(policy.key_pattern).exec(input.idempotencyKey)?.[0] !== input.idempotencyKey) reject("/idempotencyKey", "idempotency_key");
      headers[name] = idempotencyKey(input.idempotencyKey);
    }
    if (headers["content-type"] !== undefined) reject("", "content_type_owned_by_client");
    let body: string | Blob | ReadableStream<Uint8Array> | undefined, formValues: Record<string, unknown> | undefined, streamed = false;
    if (operation.body) {
      if (!Object.hasOwn(input, "body")) reject("/body", "required");
      if (operation.body.media_type === "application/x-www-form-urlencoded") {
        // JSON is only an internal scalar bridge. Its escaping allowance must
        // not inherit the unrelated JSON-body budget or reject valid URL text.
        const scalar = urlLimits(operation.limits.Form);
        const form = queryValues(codec, operation.body.fields ?? [], object(input.body, "/body"), scalar, options, operation.limits.Form, "/body");
        body = new URLSearchParams(form.pairs).toString(); formValues = form.values;
        if (form.pairs.length > operation.limits.Form.Pairs || textBytes(body) > operation.limits.Form.Bytes) reject("/body", "limit");
        headers["content-type"] = operation.body.media_type;
      }
      else if (operation.body.raw) {
        const raw = rawBody(operation.body, input.body, operation.limits.Raw?.Bytes ?? 0);
        body = raw.body; streamed = raw.stream; headers["content-type"] = raw.media;
      }
      else if (operation.body.type) { body = codec.encode(operation.body.type, input.body, operation.limits.Body); headers["content-type"] = "application/json"; }
      else { const multipart = multipartBody(codec, operation.body, input.body, operation, options); body = multipart; headers["content-type"] = multipart.type; }
    } else if (Object.hasOwn(input, "body")) reject("/body", "unknown");
    const semanticPath: Record<string, unknown> = Object.create(null);
    for (const parameter of operation.path) semanticPath[parameter.name] = codec.semantic(parameter.type, path[parameter.name], operation.limits.Body);
    const semanticBody = operation.validation && operation.body?.type ? codec.semantic(operation.body.type, input.body, operation.limits.Body) : formValues ?? input.body;
    // A raw body is opaque to client validation; the server checks it.
    const rawSkipped = operation.body?.raw !== undefined && operation.validation !== undefined;
    const validation = operation.preparation ? { issues: [], complete: false, skipped: ["request_preparation"] } satisfies ValidationReport : rawSkipped ? { issues: [], complete: false, skipped: ["raw_body"] } satisfies ValidationReport : validateRules(operation.validation, { ...input, path: semanticPath, query: query.values, body: semanticBody }, operation.limits.Validation, options.validationMessages);
    let target = base + url + (search ? "?" + search : "");
    if (operation.route.signed_url) target = signedTarget(input.signedURL, target, operation.route.signed_url, base);
    const maxBodyBytes = operation.body?.media_type === "application/x-www-form-urlencoded" ? operation.limits.Form.Bytes : operation.body?.parts ? operation.limits.Multipart.Bytes : operation.body?.raw ? operation.limits.Raw?.Bytes ?? 0 : operation.limits.Body.Bytes;
    return { operation, validation, request: { method: operation.route.method, url: target, headers, ...(body === undefined ? {} : { body }), ...(streamed ? { duplex: "half" as const } : {}), ...(call.signal ? { signal: call.signal } : {}), credentials: options.credentials ?? "same-origin", redirect: operation.redirect ? "manual" as const : "follow" as const, maxBodyBytes } satisfies HTTPRequest };
  };
  return {
    validate(name: string, request: unknown): ValidationReport { return prepare(name, request, {}).validation; },
    async invoke(name: string, request: unknown, call: CallOptions = {}): Promise<unknown> {
      const prepared = beforeSending(() => {
        const next = prepare(name, request, call);
        if (next.validation.issues.length) throw new ContractError(next.validation.issues);
        return next;
      }), { operation } = prepared;
      const response = await transport(prepared.request);
      let transferred = false, closed = false, failed = false;
      const close = async (): Promise<void> => { if (!closed) { closed = true; await response.close(); } };
      const finish = async (failed: boolean): Promise<void> => {
        if (!failed) { await close(); return; }
        try { await close(); } catch { /* Preserve the primary transport/contract failure. */ }
      };
      try {
      checkAbort(call.signal);
      // Browser fetch hides a manual redirect (opaqueredirect, status 0).
      if (operation.redirect && response.status === 0) reject("", "opaque_redirect");
      if (!Number.isInteger(response.status) || response.status < 100 || response.status > 599) reject("", "status");
      const headers = mergeHeaders(response.headers);
      if (response.status >= 400) {
        const allowed = document.errors.filter(error => operation.errors.includes(error.error_code) && error.status === response.status);
        if (!allowed.length) reject("", "status");
        if (operation.route.method === "HEAD") { await collectBody(response.body, 0, call.signal); throw new APIError(response.status, "http_error", undefined); }
        requireJSON(headers); const bytes = await collectBody(response.body, operation.limits.Response.Bytes, call.signal);
        const decoded = codec.decode(document.error_type, bytes, operation.limits.Response, tolerant), record = object(decoded);
        if (String(record.status) !== String(response.status) || typeof record.error_code !== "string" || !allowed.some(error => error.error_code === record.error_code)) reject("", "error_envelope");
        const retryText = headers["retry-after"], retry = retryText && /^[0-9]{1,3}$/.test(retryText) ? Number(retryText) : undefined;
        throw new APIError(response.status, record.error_code, decoded, retry !== undefined && retry >= 1 && retry <= 300 ? retry : undefined);
      }
      const file = operation.response?.file;
      if (file) {
        if (response.status !== operation.status && !(file.seekable && [206, 304].includes(response.status))) reject("", "status");
        if (response.status === 304 || operation.route.method === "HEAD") { await collectBody(response.body, 0, call.signal); return { status: response.status, headers, body: boundedBody(new Uint8Array(), 0), close } satisfies FileResult; }
        const media = mediaType(headers["content-type"]);
        if (!file.media_types.some(type => mediaType(type) === media) && !(response.status === 206 && media === "multipart/byteranges")) reject("", "content_type");
        let consumed = false;
        const stream = async function* (): AsyncIterable<Uint8Array> {
          if (consumed || closed) reject("", "response_consumed"); consumed = true; let failed = false;
          try { yield* boundedBody(response.body, operation.file_transfer_bytes!, call.signal); }
          catch (error) { failed = true; throw responseFailure(response.status, error); }
          finally { await finish(failed); }
        };
        transferred = true; return { status: response.status, headers, body: stream(), close } satisfies FileResult;
      }
      if (operation.response?.media_type === "text/event-stream") {
        if (response.status !== operation.status) reject("", "status");
        if (operation.route.method !== "HEAD" && mediaType(headers["content-type"]) !== "text/event-stream") reject("", "content_type");
        const type = operation.response.type!;
        let consumed = false;
        const events = async function* (): AsyncIterable<ServerEvent<unknown>> {
          if (consumed || closed) reject("", "response_consumed"); consumed = true; let failed = false;
          try { if (operation.route.method !== "HEAD") yield* serverEvents(response.body, data => codec.decode(type, data, operation.limits.Response, tolerant), operation.limits.Response.Bytes, call.signal); }
          catch (error) { failed = true; throw responseFailure(response.status, error); }
          finally { await finish(failed); }
        };
        transferred = true;
        return Object.freeze({ status: response.status, headers, [Symbol.asyncIterator]: () => events()[Symbol.asyncIterator](), close }) satisfies EventStreamResult<unknown>;
      }
      if (operation.redirect) {
        if (response.status !== operation.status) reject("", "status");
        const location = redirectLocation(headers["location"]); await collectBody(response.body, 0, call.signal);
        return Object.freeze({ status: response.status, location }) satisfies RedirectResult<number>;
      }
      const statuses = operation.statuses ?? [operation.status];
      if (!statuses.includes(response.status)) reject("", "status");
      if (!operation.response || operation.route.method === "HEAD") { await collectBody(response.body, 0, call.signal); return undefined; }
      requireJSON(headers); const decoded = codec.decode(operation.response.type!, await collectBody(response.body, operation.limits.Response.Bytes, call.signal), operation.limits.Response, tolerant);
      return operation.statuses ? Object.freeze({ status: response.status, body: decoded }) satisfies StatusResult<number, unknown> : decoded;
      } catch (error) { failed = true; throw responseFailure(response.status, error); }
      finally { if (!transferred) await finish(failed); }
    },
  };
}
// signedTarget accepts the links the route verifies: absolute links on the
// client's origin, origin-relative links (sent to the base URL), permanent links
// without an expiry when the route permits them, and declared ignored parameters.
// The signed path and query bytes are sent unchanged.
function signedTarget(input: unknown, expected: string, policy: SignedURLPolicy, base: string): string {
  if (typeof input !== "string" || !validUnicode(input) || /[\x00-\x20\x7f]/.test(input)) reject("", "signed_url");
  const relative = input.startsWith("/") && !input.startsWith("//") && !input.startsWith("/\\");
  if (!relative && !base || relative && !policy.relative) reject("", "signed_url");
  const origin = base || "https://foundry.invalid", actual = new URL(input, origin), target = new URL(expected, origin);
  if (actual.origin !== target.origin || actual.username || actual.password || actual.hash) reject("", "signed_url");
  const segments = (path: string): string[] => path.split("/").map(segment => decodeURIComponent(segment));
  if (JSON.stringify(segments(actual.pathname)) !== JSON.stringify(segments(target.pathname))) reject("", "signed_url");
  const signature = actual.searchParams.getAll(policy.signature_parameter), expires = actual.searchParams.getAll(policy.expires_parameter);
  if (signature.length !== 1 || !signature[0] || expires.length > 1 || expires.length === 1 && !expires[0] || expires.length === 0 && !policy.permanent) reject("", "signed_url");
  for (const name of [policy.expires_parameter, policy.signature_parameter, ...(policy.ignored_parameters ?? [])]) actual.searchParams.delete(name);
  const canonical = (query: URLSearchParams): string => JSON.stringify([...query.keys()].filter((key, i, keys) => keys.indexOf(key) === i).sort().map(key => [key, query.getAll(key)]));
  if (canonical(actual.searchParams) !== canonical(target.searchParams)) reject("", "signed_url");
  return relative && base ? base + input : input;
}
export { checkAbort, createHTTPInvoker, ownInput, unsentFailures, urlValue };
export type { MultipartPart, Operation, Payload, RealtimeChannel, RealtimeEvent, RuntimeDocument, URLParameter };
