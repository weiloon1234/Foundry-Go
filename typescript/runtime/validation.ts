interface RuleDescription {
  readonly kind: string; readonly field?: string; readonly other_field?: string; readonly label?: string; readonly label_key?: string;
  readonly other_label?: string; readonly other_label_key?: string;
  readonly server_only?: boolean; readonly children?: readonly RuleDescription[];
  readonly spec?: { readonly id: string; readonly message: string; readonly translation?: ValidationMessageRecipe; readonly parameters?: readonly { readonly name: string; readonly value: unknown }[] };
}
interface ValidationLimits { readonly Checks: number; readonly Depth: number; readonly Issues: number; readonly ValueBytes: number }
export interface ValidationReport { readonly issues: readonly Issue[]; readonly complete: boolean; readonly skipped: readonly string[] }
function numericText(value: unknown): string | undefined {
  if (value instanceof JSONNumber) return value.text;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  if (typeof value === "string" && /^[+-]?[0-9]+(?:\.[0-9]+)?$/.test(value)) return value;
  return undefined;
}
// Compare decimal coefficient/exponent without expanding potentially huge exponents.
function compareNumbers(left: unknown, right: unknown): number | undefined {
  const parts = (value: unknown): { sign: number; digits: string; power: bigint } | undefined => {
    const text = numericText(value); if (text === undefined) return undefined;
    const match = /^([+-]?)([0-9]+)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$/.exec(text); if (!match) return undefined;
    let digits = (match[2]! + (match[3] ?? "")).replace(/^0+/, "");
    if (!digits) return { sign: 0, digits: "", power: 0n };
    const power = BigInt(match[4] ?? "0") - BigInt(match[3]?.length ?? 0) + BigInt(digits.length);
    digits = digits.replace(/0+$/, ""); return { sign: match[1] === "-" ? -1 : 1, digits, power };
  };
  const a = parts(left), b = parts(right); if (!a || !b) return undefined;
  if (a.sign !== b.sign) return a.sign < b.sign ? -1 : 1;
  if (!a.sign) return 0;
  if (a.power !== b.power) return (a.power < b.power ? -1 : 1) * a.sign;
  const size = Math.max(a.digits.length, b.digits.length), x = a.digits.padEnd(size, "0"), y = b.digits.padEnd(size, "0");
  return (x === y ? 0 : x < y ? -1 : 1) * a.sign;
}
function validationEqual(value: unknown, candidate: unknown): boolean {
  if (candidate instanceof JSONNumber || typeof candidate === "number") return compareNumbers(value, candidate) === 0;
  return value === candidate;
}
function validateRules(description: RuleDescription | undefined, input: unknown, limits: ValidationLimits, messages?: ValidationMessages): ValidationReport {
  if (!description) return { issues: [], complete: true, skipped: [] };
  const issues: Issue[] = [], skipped = new Set<string>(); let checks = 0, truncated = false;
  const leaf = (node: RuleDescription, value: unknown): boolean | undefined => {
    const spec = node.spec!; if (node.server_only) return undefined;
    // A wire omission does not reveal the Go selector's zero-value behavior.
    // Presence-aware rules can decide; other leaves remain server checks.
    if (value === undefined && !["foundry.absent", "foundry.required", "foundry.prohibited", "foundry.not_nil"].includes(spec.id)) return undefined;
    const parameters: Record<string, unknown> = Object.create(null);
    for (const p of spec.parameters ?? []) parameters[p.name] = p.value;
    if (typeof value === "string" && (textBytes(value) > limits.ValueBytes || !validUnicode(value))) reject("", "limit");
    const count = typeof value === "string" ? [...value].length : Array.isArray(value) ? value.length : value === null ? 0 : -1;
    const empty = (): boolean => parameters.empty_kind === "text" ? typeof value === "string" && trimGoSpace(value) === "" : parameters.empty_kind === "collection" ? value === null || typeof value === "object" && value !== null && Object.keys(value).length === 0 : false;
    switch (spec.id) {
      case "foundry.absent": return value === undefined;
      case "foundry.not_nil": return value !== null && value !== undefined;
      case "foundry.required": return value !== undefined && value !== null && !empty();
      case "foundry.prohibited": return value === undefined || value === null || empty();
      case "foundry.non_empty": return !empty();
      case "foundry.empty": return empty();
      case "foundry.non_blank": return typeof value === "string" && trimGoSpace(value) !== "";
      case "foundry.min_length": case "foundry.min_items": return count >= 0 && compareNumbers(count, parameters.min)! >= 0;
      case "foundry.max_length": case "foundry.max_items": return count >= 0 && compareNumbers(count, parameters.max)! <= 0;
      case "foundry.min": case "foundry.decimal_min": { const result = compareNumbers(value, parameters.min); return result !== undefined && result >= 0; }
      case "foundry.max": case "foundry.decimal_max": { const result = compareNumbers(value, parameters.max); return result !== undefined && result <= 0; }
      case "foundry.one_of": case "foundry.not_one_of": case "foundry.enum": {
        if (!Array.isArray(parameters.values)) return undefined;
        const match = parameters.values.some(candidate => validationEqual(value, candidate)); return spec.id === "foundry.not_one_of" ? !match : match;
      }
      case "foundry.same": case "foundry.different": {
        if (!Array.isArray(value) || value.length !== 2) return false;
        const match = validationEqual(value[0], value[1]); return spec.id === "foundry.same" ? match : !match;
      }
      case "foundry.starts_with": return typeof value === "string" && typeof parameters.value === "string" && value.startsWith(parameters.value);
      case "foundry.ends_with": return typeof value === "string" && typeof parameters.value === "string" && value.endsWith(parameters.value);
      case "foundry.digits": return typeof value === "string" && /^[0-9]*$/.test(value);
      case "foundry.uuid": return typeof value === "string" && uuidPattern.test(value);
      case "foundry.date": return typeof value === "string" && dateValid(value);
      case "foundry.time": return typeof value === "string" && timeValid(value);
      case "foundry.datetime": return typeof value === "string" && dateTimeValid(value);
      case "foundry.local_datetime": return typeof value === "string" && formatValid("local_date_time", value);
      case "foundry.distinct": {
        if (!Array.isArray(value)) return false;
        const seen = new Set<unknown>();
        for (const item of value) { if (++checks > limits.Checks) reject("", "limit"); const key = item instanceof JSONNumber ? numericIdentity(item.text) : item; if (seen.has(key)) return false; seen.add(key); } return true;
      }
      default: return undefined;
    }
  };
  const visit = (node: RuleDescription, value: unknown, path: string, depth: number, label = "", labelKey = "", field = "", other = "", otherKey = ""): boolean | undefined => {
    if (++checks > limits.Checks || depth > limits.Depth) reject(path, "limit");
    if (issues.length >= limits.Issues) { truncated = true; return undefined; }
    const children = node.children ?? [];
    switch (node.kind) {
      case "rule": {
        const valid = leaf(node, value);
        if (valid === undefined) skipped.add(node.spec!.id);
        else if (!valid) {
          const display = validationLabel(messages, labelKey, label);
          const attribute = field || label || labelKey ? validationLabel(messages, labelKey, label || field) : undefined;
          issues.push({ path, code: node.spec!.id, message: validationMessage(node.spec!, attribute, other ? validationLabel(messages, otherKey, other) : undefined, messages), ...(display ? { label: display } : {}), ...(labelKey ? { label_key: labelKey } : {}) });
        }
        return valid;
      }
      case "field": {
        const record = value !== undefined && value !== null ? object(value, path) : undefined;
        return visit(children[0]!, record && Object.hasOwn(record, node.field!) ? record[node.field!] : undefined, pointer(path, node.field!), depth + 1, node.label, node.label_key, node.field);
      }
      case "compare": { const record = object(value, path); return visit(children[0]!, [Object.hasOwn(record, node.field!) ? record[node.field!] : undefined, Object.hasOwn(record, node.other_field!) ? record[node.other_field!] : undefined], pointer(path, node.field!), depth + 1, node.label, node.label_key, node.field, node.other_label || node.other_field, node.other_label_key); }
      case "optional": if (value === undefined) return true; return visit(children[0]!, value, path, depth + 1, label, labelKey, field, other, otherKey);
      case "nullable": case "pointer": if (value === null || value === undefined) return true; return visit(children[0]!, value, path, depth + 1, label, labelKey, field, other, otherKey);
      case "when": case "unless": {
        const before = issues.length, skippedBefore = skipped.size, matched = visit(children[0]!, value, path, depth + 1, label, labelKey, field, other, otherKey); issues.splice(before);
        if (matched === undefined || skipped.size !== skippedBefore) return undefined;
        return matched === (node.kind === "when") ? visit(children[1]!, value, path, depth + 1, label, labelKey, field, other, otherKey) : true;
      }
      case "all": case "bail": {
        let result: boolean | undefined = true;
        for (const child of children) { const next = visit(child, value, path, depth + 1, label, labelKey, field, other, otherKey); if (next === false) result = false; else if (next === undefined && result === true) result = undefined; if (node.kind === "bail" && next !== true) break; }
        return result;
      }
      case "each": {
        if (value === null) return true;
        if (!Array.isArray(value)) { skipped.add("each"); return undefined; }
        let result: boolean | undefined = true;
        for (let i = 0; i < value.length; i++) { const next = visit(children[0]!, value[i], pointer(path, String(i)), depth + 1, label, labelKey, field, other, otherKey); if (next === false) result = false; else if (next === undefined && result === true) result = undefined; }
        return result;
      }
      default: skipped.add(node.kind); return undefined;
    }
  };
  const result = visit(description, input, "", 0);
  return { issues, complete: !truncated && skipped.size === 0 && result !== undefined, skipped: [...skipped].sort() };
}
function numericIdentity(text: string): string {
  const match = /^(-?)([0-9]+)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$/.exec(text)!;
  let digits = (match[2]! + (match[3] ?? "")).replace(/^0+/, ""), power = BigInt(match[4] ?? "0") - BigInt(match[3]?.length ?? 0);
  if (!digits) return "number:0";
  while (digits.endsWith("0")) { digits = digits.slice(0, -1); power++; }
  return "number:" + match[1] + digits + "e" + power;
}
export type { RuleDescription };
