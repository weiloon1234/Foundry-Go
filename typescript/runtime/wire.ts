// Shared generated runtime. Transport adapters use raw JSON, never JSON.parse on payloads.
/** Bounds one wire document; Nodes counts containers, values and object names. */
export interface JSONLimits { readonly Bytes: number; readonly Depth: number; readonly Nodes: number; readonly Steps: number; readonly Issues: number }
export interface Issue { readonly path: string; readonly code: string; readonly message?: string; readonly label?: string; readonly label_key?: string }
export class ContractError extends Error {
  constructor(readonly issues: readonly Issue[] = [{ path: "", code: "invalid" }]) { super("Invalid client contract value"); this.name = "ContractError"; }
}
function reject(path = "", code = "invalid"): never { throw new ContractError([{ path, code }]); }
function pointer(path: string, key: string): string { return path + "/" + key.replace(/~/g, "~0").replace(/\//g, "~1"); }
const encoder = /* @__PURE__ */ new TextEncoder();
const decoder = /* @__PURE__ */ new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const numberPattern = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/;
const integerPattern = /^-?(?:0|[1-9][0-9]*)$/;

/** An exact JSON number in an explicitly dynamic payload. No implicit JS coercion. */
export class JSONNumber {
  readonly text: string;
  constructor(text: string) { if (!numberPattern.test(text)) reject(); this.text = text; Object.freeze(this); }
}
export type JSONValue = null | boolean | string | JSONNumber | readonly JSONValue[] | { readonly [name: string]: JSONValue };
/** A server enum value this client does not know. Tolerant decoding surfaces it instead of failing; it cannot be sent back. */
export class UnknownEnumValue {
  constructor(readonly type: string, readonly value: string) { Object.freeze(this); }
}
function textBytes(text: string): number { return encoder.encode(text).byteLength; }
function validUnicode(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    const code = text.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) { const next = text.charCodeAt(++i); if (!(next >= 0xdc00 && next <= 0xdfff)) return false; }
    else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}
function limitsValid(limits: JSONLimits): void {
  for (const value of [limits.Bytes, limits.Nodes, limits.Steps, limits.Issues]) if (!Number.isSafeInteger(value) || value < 1) reject("", "limit");
  if (!Number.isSafeInteger(limits.Depth) || limits.Depth < 0 || limits.Depth > runtimePolicy.maxDepth) reject("", "limit");
}
function rawText(input: string | Uint8Array, max: number): string {
  if (input instanceof Uint8Array) { if (input.byteLength > max) reject("", "limit"); try { return decoder.decode(input); } catch { reject(); } }
  if (typeof input !== "string" || input.length > max || !validUnicode(input) || textBytes(input) > max) reject("", "limit");
  return input;
}
function parseWire(input: string | Uint8Array, limits: JSONLimits): JSONValue {
  limitsValid(limits);
  const source = rawText(input, limits.Bytes); let at = 0, nodes = 0;
  const whitespace = (): void => { while (at < source.length && /[\x20\t\r\n]/.test(source[at]!)) at++; };
  const string = (): string => {
    const start = at++;
    while (at < source.length) {
      const c = source[at++];
      if (c === '"') {
        let value: unknown; try { value = JSON.parse(source.slice(start, at)); } catch { reject(); }
        if (typeof value !== "string" || !validUnicode(value)) reject(); return value;
      }
      if (c === "\\") at++;
    }
    reject();
  };
  const read = (depth: number): JSONValue => {
    if (++nodes > limits.Nodes || depth > limits.Depth) reject("", "limit");
    whitespace(); const first = source[at];
    if (first === '"') return string();
    if (first === "[") {
      at++; whitespace(); const value: JSONValue[] = [];
      if (source[at] === "]") { at++; return value; }
      for (;;) { value.push(read(depth + 1)); whitespace(); const end = source[at++]; if (end === "]") return value; if (end !== ",") reject(); }
    }
    if (first === "{") {
      at++; whitespace(); const value: Record<string, JSONValue> = Object.create(null);
      if (source[at] === "}") { at++; return value; }
      for (;;) {
        whitespace(); if (source[at] !== '"') reject();
        if (++nodes > limits.Nodes) reject("", "limit");
        const key = string();
        if (Object.hasOwn(value, key)) reject("", "duplicate");
        whitespace(); if (source[at++] !== ":") reject(); value[key] = read(depth + 1);
        whitespace(); const end = source[at++]; if (end === "}") return value; if (end !== ",") reject();
      }
    }
    for (const [token, value] of [["null", null], ["true", true], ["false", false]] as const) {
      if (source.startsWith(token, at)) { at += token.length; return value; }
    }
    const start = at;
    while (at < source.length && /[-+0-9.eE]/.test(source[at]!)) at++;
    if (at === start) reject(); return new JSONNumber(source.slice(start, at));
  };
  const value = read(0); whitespace(); if (at !== source.length) reject(); return value;
}
function object(value: unknown, path = ""): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value) || value instanceof JSONNumber) reject(path, "type");
  const prototype = Object.getPrototypeOf(value);
  if (prototype !== null && prototype !== Object.prototype) reject(path, "type");
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== "string" || !validUnicode(key)) reject(path, "type");
    const property = Object.getOwnPropertyDescriptor(value, key)!;
    if (!("value" in property) || !property.enumerable) reject(path, "type");
  }
  return value as Record<string, unknown>;
}
function writeWire(value: unknown, limits: JSONLimits): string {
  limitsValid(limits); let nodes = 0, bytes = 0; const chunks: string[] = [];
  const add = (text: string): void => { bytes += textBytes(text); if (bytes > limits.Bytes) reject("", "limit"); chunks.push(text); };
  const visit = (value: unknown, depth: number): void => {
    if (++nodes > limits.Nodes || depth > limits.Depth) reject("", "limit");
    if (value === null || typeof value === "boolean") { add(String(value)); return; }
    if (typeof value === "string") { if (!validUnicode(value)) reject(); add(JSON.stringify(value)); return; }
    if (value instanceof JSONNumber) { if (!numberPattern.test(value.text)) reject(); add(value.text); return; }
    if (Array.isArray(value)) {
      if (value.length > limits.Nodes - nodes) reject("", "limit");
      add("["); for (let i = 0; i < value.length; i++) { if (i) add(","); visit(value[i], depth + 1); } add("]"); return;
    }
    const record = object(value); add("{"); let first = true;
    for (const key of Object.keys(record).sort()) {
      if (++nodes > limits.Nodes) reject("", "limit");
      if (!first) add(","); first = false; add(JSON.stringify(key) + ":"); visit(record[key], depth + 1);
    } add("}");
  };
  visit(value, 0); return chunks.join("");
}
/** Public display hints; they neither validate nor supply default values. */
export interface Presentation {
  readonly kind?: PresentationKind;
  readonly label_key?: string;
  readonly help_key?: string;
}
interface Property { readonly name: string; readonly type: string; readonly required: boolean; readonly presentation?: Presentation }
interface UnionVariant { readonly tag: string; readonly type: string }
interface WireType {
  readonly discriminator?: string; readonly variants?: readonly UnionVariant[];
  readonly id: string; readonly kind: string; readonly nullable: boolean; readonly properties?: readonly Property[];
  readonly element?: string; readonly length?: number; readonly bits?: number; readonly signed?: boolean; readonly format?: string;
  readonly cases?: readonly unknown[]; readonly key?: { readonly value: WireType; readonly syntax: string; readonly server_only: boolean; readonly non_zero: boolean };
}
/** Custom URL syntax must be provided explicitly. Payload schema validation still runs. */
export interface URLCodec { format(value: unknown): string }
export interface CodecOptions { readonly urlCodecs?: Readonly<Record<string, URLCodec>> }
interface RuntimePolicy { readonly maxDepth: number; readonly decimalDigits: number; readonly metadataBytes: number; readonly messageBytes: number }
// runtimePolicy is emitted from Go-owned constants beside the manifest.

class WireCodec {
  private readonly types = new Map<string, WireType>();
  private readonly variants = new Map<string, Map<string, string>>();
  private readonly properties = new Map<string, ReadonlySet<string>>();
  constructor(types: readonly WireType[]) {
    for (const type of types) {
      this.types.set(type.id, type);
      if (type.kind === "union") this.variants.set(type.id, new Map(type.variants!.map(v => [v.tag, v.type])));
      if (type.kind === "object") this.properties.set(type.id, new Set((type.properties ?? []).map(p => p.name)));
    }
  }
  type(id: string): WireType { const type = this.types.get(id); if (!type) reject("", "schema"); return type; }
  /** Tolerant decoding ignores unknown object properties and surfaces unknown enum values; requests stay strict. */
  decode(id: string, input: string | Uint8Array, limits: JSONLimits, tolerant = false): unknown { return this.transform(id, parseWire(input, limits), limits, false, false, tolerant); }
  encode(id: string, input: unknown, limits: JSONLimits): string { return writeWire(this.transform(id, input, limits, true), limits); }
  semantic(id: string, input: unknown, limits: JSONLimits): unknown { return this.transform(id, input, limits, true, true); }
  transform(id: string, input: unknown, limits: JSONLimits, encode: boolean, semantic = false, tolerant = false): unknown {
    tolerant = tolerant && !encode;
    limitsValid(limits); let steps = 0;
    const visit = (id: string, value: unknown, path: string, depth: number): unknown => {
      if (depth > limits.Depth) reject(path, "limit");
      let type: WireType;
      for (;;) {
        if (++steps > limits.Steps) reject(path, "limit"); type = this.type(id);
        if (value === null) { if (!type.nullable) reject(path, "null"); return null; }
        if (type.kind !== "alias") break; id = type.element!;
      }
      switch (type.kind) {
        case "dynamic": return parseWire(writeWire(value, limits), limits);
        case "quoted": {
          if (encode) { const inner = visit(type.element!, value, path, depth); if (inner === null) reject(path, "null"); return semantic ? inner : writeWire(inner, limits); }
          if (typeof value !== "string") reject(path, "type");
          const inner = parseWire(value, { ...limits, Depth: 0, Nodes: 1 }); if (inner === null) reject(path, "null");
          return visit(type.element!, inner, path, depth);
        }
        case "union": {
          const record = object(value, path), discriminator = type.discriminator!, tagPath = pointer(path, discriminator);
          if (!Object.hasOwn(record, discriminator)) reject(tagPath, "required");
          const tag = record[discriminator];
          if (typeof tag !== "string") reject(tagPath, "type");
          const target = this.variants.get(id)?.get(tag);
          if (target === undefined) reject(tagPath, "value");
          const payload: Record<string, unknown> = Object.create(null);
          for (const key of Object.keys(record)) {
            if (++steps > limits.Steps) reject(path, "limit");
            if (key !== discriminator) payload[key] = record[key];
          }
          const result = visit(target, payload, path, depth) as Record<string, unknown>;
          result[discriminator] = tag;
          return result;
        }
        case "object": {
          const record = object(value, path), properties = type.properties ?? [], known = this.properties.get(id)!;
          const result: Record<string, unknown> = Object.create(null);
          for (const key of Object.keys(record)) { if (++steps > limits.Steps) reject(path, "limit"); if (!known.has(key) && !tolerant) reject(pointer(path, key), "unknown"); }
          for (const property of properties) {
            if (++steps > limits.Steps) reject(path, "limit");
            const child = pointer(path, property.name);
            if (!Object.hasOwn(record, property.name)) { if (property.required) reject(child, "required"); continue; }
            result[property.name] = visit(property.type, record[property.name], child, depth + 1);
          }
          return result;
        }
        case "array": {
          if (!Array.isArray(value)) reject(path, "type");
          if (type.length !== undefined && value.length !== type.length) reject(path, "length");
          if (value.length > limits.Nodes || value.length > limits.Steps - steps) reject(path, "limit");
          return value.map((child, i) => visit(type.element!, child, pointer(path, String(i)), depth + 1));
        }
        case "map": {
          const record = object(value, path), result: Record<string, unknown> = Object.create(null), key = type.key;
          for (const name of Object.keys(record).sort()) {
            if (++steps > limits.Steps) reject(path, "limit");
            // A tolerant client omits entries keyed by enum cases it does not know.
            if (key !== undefined && !this.mapKey(key, name, path, tolerant)) continue;
            result[name] = visit(type.element!, record[name], pointer(path, name), depth + 1);
          }
          return result;
        }
        default: return this.scalar(type, value, encode, path, tolerant);
      }
    };
    return visit(id, input, "", 0);
  }
  /** Checks one declared map key; false only for an unknown enum key a tolerant decode omits. */
  mapKey(key: NonNullable<WireType["key"]>, name: string, path: string, tolerant = false): boolean {
    let checked: unknown;
    if (key.value.kind === "integer") {
      if (!/^(?:0|-?[1-9][0-9]*)$/.test(name)) reject(pointer(path, name), "key");
      checked = this.scalar(key.value, new JSONNumber(name), false, path, tolerant);
    } else checked = this.scalar(key.value, name, false, path, tolerant);
    if (checked instanceof UnknownEnumValue) return false;
    if (key.syntax === "model_id" && name !== name.toLowerCase()) reject(pointer(path, name), "key");
    if (key.non_zero && name.toLowerCase() === "00000000-0000-0000-0000-000000000000") reject(pointer(path, name), "key");
    return true;
  }
  private scalar(type: WireType, value: unknown, encode: boolean, path: string, tolerant = false): unknown {
    let wire: unknown = value, result: unknown = value;
    switch (type.kind) {
      case "boolean": if (typeof value !== "boolean") reject(path, "type"); break;
      case "string": if (typeof value !== "string" || !validUnicode(value) || !formatValid(type.format, value)) reject(path, "value"); break;
      case "integer": {
        const wide = type.bits! > 32;
        if (encode) {
          if (wide ? typeof value !== "string" : typeof value !== "number" || !Number.isSafeInteger(value)) reject(path, "type");
          wire = new JSONNumber(String(value));
        }
        if (!(wire instanceof JSONNumber) || !integerPattern.test(wire.text) || wire.text.length > 21 || (!type.signed && wire.text.startsWith("-"))) reject(path, "value");
        const integer = BigInt(wire.text), bits = BigInt(type.bits!), bound = 1n << (bits - (type.signed ? 1n : 0n));
        if (integer < (type.signed ? -bound : 0n) || integer >= bound) reject(path, "value");
        result = wide ? integer.toString() : Number(integer); break;
      }
      case "number": {
        const exact = !type.bits;
        if (encode) { if (exact ? typeof value !== "string" : typeof value !== "number" || !Number.isFinite(value)) reject(path, "type"); wire = new JSONNumber(String(value)); }
        if (!(wire instanceof JSONNumber)) reject(path, "type");
        result = exact ? wire.text : Number(wire.text);
        if (!exact && (!Number.isFinite(result as number) || type.bits === 32 && !Number.isFinite(Math.fround(result as number)))) reject(path, "value");
        break;
      }
      default: reject(path, "schema");
    }
    if (type.cases?.length) {
      const match = type.cases.some(candidate => type.kind === "integer" ? BigInt(String(candidate instanceof JSONNumber ? candidate.text : candidate)) === BigInt((wire as JSONNumber).text) : candidate === value);
      if (!match) {
        if (tolerant) return new UnknownEnumValue(type.id, wire instanceof JSONNumber ? wire.text : String(value));
        reject(path, "value");
      }
    }
    return encode ? wire : result;
  }
}
// Only decoded, bounded JSON trees pass here. Freezing prevents one event
// listener from changing another listener's view of the same publication.
function immutable<T>(value: T): T {
  if (value !== null && typeof value === "object" && !Object.isFrozen(value)) {
    for (const child of Object.values(value)) immutable(child);
    Object.freeze(value);
  }
  return value;
}
export { WireCodec, immutable, integerPattern, limitsValid, numberPattern, object, parseWire, pointer, reject, textBytes, validUnicode, writeWire };
export type { WireType };
