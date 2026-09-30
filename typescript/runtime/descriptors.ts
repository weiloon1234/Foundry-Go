/** Public display hints; they neither validate nor supply default values. */
export interface Presentation {
  readonly kind?: PresentationKind;
  readonly label_key?: string;
  readonly help_key?: string;
}
/** Unsafe-size metadata numbers stay exact instead of being rounded or clamped. */
export type ExactMetadataNumber = number | JSONNumber;
type ExactMetadata<T> = T extends number ? ExactMetadataNumber : T extends readonly (infer V)[] ? readonly ExactMetadata<V>[] : T extends object ? { readonly [K in keyof T]: ExactMetadata<T[K]> } : T;
export type ParameterMetadata = Omit<URLParameter, "type" | "syntax"> & { readonly type?: string; readonly syntax?: string; readonly kind?: string };
export type PayloadMetadata = Omit<Payload, "parts"> & { readonly parts?: readonly ParameterMetadata[] };
export type OperationMetadata = Omit<Operation, "limits" | "file_transfer_bytes" | "body" | "response"> & { readonly body?: PayloadMetadata; readonly response?: PayloadMetadata; readonly limits: ExactMetadata<Operation["limits"]>; readonly file_transfer_bytes?: ExactMetadataNumber };
export type SchemaMetadata = Readonly<WireType>;
export type ValidationMetadata = Readonly<RuleDescription>;
type IsUnion<T, Whole = T> = T extends Whole ? [Whole] extends [T] ? false : true : never;
type MapKeys<T> = Extract<keyof NonNullable<T>, string>;
/** Maps keyed by model IDs have branded Identity keys, not property names. */
type IdentityKeyed<T> = [MapKeys<T>] extends [never] ? false : [MapKeys<T>] extends [Identity<string>] ? true : false;
type ObjectFields<T> = NonNullable<T> extends string | number | boolean | readonly unknown[] | Blob | Upload ? never : true extends IsUnion<NonNullable<T>> ? never : string extends keyof NonNullable<T> ? never : IdentityKeyed<T> extends true ? never : MapKeys<T>;
type Discriminator<T> = { [K in Extract<keyof NonNullable<T>, string>]: NonNullable<T>[K] extends string ? string extends NonNullable<T>[K] ? never : K : never }[Extract<keyof NonNullable<T>, string>];
type IsCollection<T> = NonNullable<T> extends readonly unknown[] ? true : string extends keyof NonNullable<T> ? true : IdentityKeyed<T>;
type CollectionKey<T> = NonNullable<T> extends readonly unknown[] ? number : IdentityKeyed<T> extends true ? MapKeys<T> : string;
type ElementValue<T> = NonNullable<T> extends readonly (infer V)[] ? V : Exclude<NonNullable<T>[MapKeys<T>], undefined>;
declare const descriptorType: unique symbol;
/** Value/owner identity prevents fields of different operations or types being interchanged. */
export interface FieldDescriptor<T, Owner = unknown> {
  readonly [descriptorType]: (value: T, owner: Owner) => readonly [T, Owner];
  readonly path: string;
  readonly required: boolean;
  readonly nullable: boolean;
  readonly repeated: boolean;
  readonly presentation: Readonly<Presentation>;
  readonly schema: SchemaMetadata | undefined;
  readonly parameter: ParameterMetadata | undefined;
  readonly choices: readonly NonNullable<T>[];
  field<K extends ObjectFields<T>>(name: K): FieldDescriptor<NonNullable<T>[K], readonly [Owner, K]>;
  /** Select a declared union payload; the discriminator stays on the parent. */
  variant<D extends Discriminator<T>, V extends Extract<NonNullable<T>[D], string>>(discriminator: D, tag: V): FieldDescriptor<Omit<Extract<NonNullable<T>, Record<D, V>>, D>, readonly [Owner, D, V]>;
  /** Bind one concrete array index or map key; element() remains a template. */
  at(this: IsCollection<T> extends true ? FieldDescriptor<T, Owner> : never, key: CollectionKey<T>): FieldDescriptor<ElementValue<T>, readonly [Owner, "element"]>;
  element(this: IsCollection<T> extends true ? FieldDescriptor<T, Owner> : never): FieldDescriptor<ElementValue<T>, readonly [Owner, "element"]>;
}
export type FieldValue<D> = D extends FieldDescriptor<infer T, infer _Owner> ? T : never;
export interface OperationDescriptor<K extends keyof Operations> {
  readonly name: K;
  readonly metadata: OperationMetadata;
  /** This tree and preparation flag are descriptions, not a validation verdict. */
  readonly validation: ValidationMetadata | undefined;
  readonly preparation: boolean;
  field<L extends Extract<keyof OperationInputs[K], string>, F extends ObjectFields<OperationInputs[K][L]>>(location: L, name: F): FieldDescriptor<NonNullable<OperationInputs[K][L]>[F], readonly [K, L, F]>;
  validate(input: Operations[K]["request"], options?: ClientOptions): ValidationReport;
  call(client: API, input: Operations[K]["request"], options?: CallOptions): Promise<Operations[K]["response"]>;
}

let descriptorDocumentCache: RuntimeDocument | undefined;
let descriptorTypesCache: ReadonlyMap<string, WireType> | undefined;
function descriptorDocument(): RuntimeDocument {
  // An independent, lossless snapshot keeps public inspection from mutating the
  // invoker's internal metadata. Its numeric limits are not operational inputs.
  return descriptorDocumentCache ??= immutable(loadRuntimeDocument(false));
}
function descriptorTypes(): ReadonlyMap<string, WireType> {
  return descriptorTypesCache ??= new Map(descriptorDocument().types.map(type => [type.id, type]));
}
function resolvedDescriptor(id: string): { type: WireType; nullable: boolean } {
  let type = descriptorTypes().get(id);
  if (!type) reject("", "unknown_schema");
  // Null is accepted or rejected at the outer codec before following aliases.
  const nullable = type.nullable;
  for (let depth = 0; ; depth++) {
    if (depth > descriptorTypes().size) reject("", "schema_cycle");
    if (type.kind !== "alias") return { type, nullable };
    type = descriptorTypes().get(type.element!);
    if (!type) reject("", "unknown_schema");
  }
}

// Choices depend only on their type. Decode each type's cases once instead of on
// every navigation; enums may be large and decoding checks case membership.
const descriptorChoicesCache = new Map<string, readonly unknown[]>();
function descriptorChoices(id: string, type: WireType): readonly unknown[] {
  const cached = descriptorChoicesCache.get(id);
  if (cached) return cached;
  const quoted = type.kind === "quoted";
  const cases = (quoted ? resolvedDescriptor(type.element!).type : type).cases ?? [];
  const limits = metadataLimits();
  const choices = Object.freeze(cases.map(value => {
    const wire = writeWire(typeof value === "number" ? new JSONNumber(String(value)) : value, limits);
    return contracts().decode(id, quoted ? writeWire(wire, limits) : wire, limits);
  }));
  descriptorChoicesCache.set(id, choices);
  return choices;
}

interface DescriptorReference {
  readonly operation: string;
  readonly segments: readonly (string | number | null)[];
  readonly guards: readonly { readonly segments: readonly (string | number | null)[]; readonly discriminator: string; readonly tag: string }[];
  readonly id?: string;
}
const descriptorReferences = new WeakMap<object, DescriptorReference>();
function childReference(reference: DescriptorReference | undefined, key: string | number | null): DescriptorReference | undefined {
  return reference && { ...reference, segments: [...reference.segments, key] };
}

function describeField(id: string | undefined, required: boolean, path: string, hint?: Presentation, parameter?: URLParameter | MultipartPart, repeated = false, reference?: DescriptorReference): unknown {
  const resolved = id ? resolvedDescriptor(id) : undefined;
  const type = resolved?.type;
  const file = parameter && "kind" in parameter && parameter.kind === "file";
  if (!type && !file) reject(path, "unknown_schema");
  // Enum choices use the existing codec's exact client representation, including
  // wide integers as strings. No enum validator is recreated by the descriptor.
  const choices = !repeated && type ? descriptorChoices(id!, type) : [];
  const descriptor = immutable({
    path, required, nullable: !repeated && (resolved?.nullable ?? false), repeated,
    presentation: hint ?? {}, schema: type, parameter, choices,
    field(name: string): unknown {
      if (typeof name !== "string" || repeated) reject(path, "unknown_field");
      if (type?.kind === "map" && type.key?.value.cases?.some(value => String(value instanceof JSONNumber ? value.text : value) === name)) {
        return describeField(type.element, false, pointer(path, name), undefined, undefined, false, childReference(reference, name));
      }
      if (type?.kind !== "object") reject(path, "unknown_field");
      const property = type.properties?.find(property => property.name === name);
      if (!property) reject(path, "unknown_field");
      return describeField(property.type, property.required, pointer(path, name), property.presentation, undefined, false, childReference(reference, name));
    },
    variant(discriminator: string, tag: string): unknown {
      if (repeated || type?.kind !== "union" || discriminator !== type.discriminator || typeof tag !== "string") reject(path, "unknown_variant");
      const variant = type.variants?.find(variant => variant.tag === tag);
      if (!variant) reject(path, "unknown_variant");
      return describeField(variant.type, true, path, undefined, undefined, false, reference && { ...reference, guards: [...reference.guards, { segments: reference.segments, discriminator, tag }] });
    },
    at(key: string | number): unknown {
      const array = repeated || type?.kind === "array";
      if (array) {
        if (!Number.isSafeInteger(key) || (key as number) < 0 || !repeated && type!.length !== undefined && (key as number) >= type!.length) reject(path, "collection_key");
      } else {
        if (type?.kind !== "map" || typeof key !== "string" || !validUnicode(key) || key.length > defaultJSONLimits.Bytes) reject(path, "collection_key");
        // Declared keys follow the codec's own rules: enum cases, integer
        // syntax and lowercase non-zero model IDs.
        if (type.key) contracts().mapKey(type.key, key, path);
      }
      return repeated ? describeField(id, true, pointer(path, String(key)), hint, parameter, false, childReference(reference, key))
        : describeField(type!.element, array, pointer(path, String(key)), undefined, undefined, false, childReference(reference, key));
    },
    element(): unknown {
      if (repeated) return describeField(id, true, pointer(path, "*"), hint, parameter, false, childReference(reference, null));
      if (type?.kind !== "array" && type?.kind !== "map") reject(path, "not_collection");
      return describeField(type.element, true, pointer(path, "*"), undefined, undefined, false, childReference(reference, null));
    },
  });
  if (reference) descriptorReferences.set(descriptor, { ...reference, ...(id ? { id } : {}) });
  return descriptor;
}

function describeOperation(name: string): unknown {
  if (typeof name !== "string") reject("", "unknown_operation");
  const op = descriptorDocument().http.find(op => op.name === name);
  if (!op) reject("", "unknown_operation");
  return Object.freeze({
    name, metadata: op, validation: op.validation, preparation: op.preparation === true,
    field(location: string, name: string): unknown {
      if (typeof name !== "string") reject("", "unknown_field");
      let parameters: readonly (URLParameter | MultipartPart)[] | undefined;
      switch (location) {
        case "path": parameters = op.path; break;
        case "query": parameters = op.query; break;
        case "body":
          if (op.body?.type) {
            const descriptor = describeField(op.body.type, true, "/body", undefined, undefined, false, { operation: op.name, segments: ["body"], guards: [] }) as { field(name: string): unknown };
            return descriptor.field(name);
          }
          parameters = op.body?.parts ?? op.body?.fields; break;
        default: reject("", "unknown_location");
      }
      const parameter = parameters?.find(field => field.name === name);
      if (!parameter) reject("", "unknown_field");
      return describeField(parameter.type || undefined, parameter.required, pointer("/" + location, name), parameter.presentation, parameter, parameter.repeated, { operation: op.name, segments: [location, name], guards: [] });
    },
    validate(input: unknown, options: ClientOptions = {}): ValidationReport {
      return validateRequest(name as keyof Operations, input as Operations[keyof Operations]["request"], options);
    },
    call(client: API, input: unknown, options?: CallOptions): unknown {
      const invoke = client?.[name as keyof API];
      if (typeof invoke !== "function") reject("", "unknown_operation");
      return (invoke as (input: unknown, options?: CallOptions) => unknown).call(client, input, options);
    },
  });
}
