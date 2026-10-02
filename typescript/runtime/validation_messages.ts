export type ValidationPluralForm = "zero" | "one" | "two" | "few" | "many" | "other";
export type ValidationTemplate = string | Readonly<Partial<Record<ValidationPluralForm, string>> & { other: string }>;
export interface ValidationMessageCatalog {
  readonly locale: string;
  readonly translations: Readonly<Record<string, ValidationTemplate>>;
}
/** Public catalog text only. Missing/invalid translations use the exported English recipe. */
export interface ValidationMessages extends ValidationMessageCatalog { readonly fallback?: ValidationMessageCatalog }
export interface ValidationMessageRecipe {
  readonly definition: { readonly key: string; readonly parameters?: readonly { readonly name: string; readonly kind: "text" | "number" | "boolean" }[]; readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" };
  readonly arguments?: readonly { readonly name: string; readonly kind: string; readonly value: string }[];
  readonly fallback: Readonly<Partial<Record<ValidationPluralForm, string>> & { other: string }>;
  readonly literal_fallback?: boolean;
}
const maxPluralFractionDigits = 20;
function messageText(text: unknown): text is string { return typeof text === "string" && text.length <= runtimePolicy.messageBytes && textBytes(text) <= runtimePolicy.messageBytes && validUnicode(text) && !text.includes("\0"); }
function validationLabel(messages: ValidationMessages | undefined, key: string, fallback: string): string {
  if (key) for (const catalog of [messages, messages?.fallback]) {
    if (!catalog || !Object.hasOwn(catalog.translations, key)) continue;
    const text = catalog.translations[key];
    if (messageText(text) && !text.includes("{{") && !text.includes("}}")) return text;
  }
  return fallback;
}
function validationMessage(spec: NonNullable<RuleDescription["spec"]>, attribute: string | undefined, other: string | undefined, messages?: ValidationMessages): string {
  const recipe = spec.translation; if (!recipe) return spec.message;
  const args: Record<string, string> = Object.create(null);
  for (const item of recipe.arguments ?? []) args[item.name] = item.value;
  if (attribute !== undefined && Object.hasOwn(args, "attribute")) args.attribute = attribute;
  if (other !== undefined && Object.hasOwn(args, "other")) args.other = other;
  const render = (template: ValidationTemplate, locale: string, plural = true): string | undefined =>
    renderMessage(template, locale, args, plural ? recipe.definition.plural : undefined, recipe.definition.plural_kind);
  for (const catalog of [messages, messages?.fallback]) {
    if (!catalog || !Object.hasOwn(catalog.translations, recipe.definition.key)) continue;
    const rendered = render(catalog.translations[recipe.definition.key]!, catalog.locale);
    if (rendered !== undefined) return rendered;
  }
  if (recipe.literal_fallback) return recipe.fallback.other;
  // Built-in English plurals depend on exact count == 1; their string arguments
  // retain precision even outside Intl's safe range.
  const form = recipe.definition.plural && compareNumbers(args[recipe.definition.plural]!.replace(/^-/, ""), "1") === 0 ? "one" : "other";
  const template = recipe.fallback[form] ?? recipe.fallback.other;
  return render(template, "en", false) ?? spec.message;
}
// renderMessage applies one catalog template: plural form selection with
// Intl.PluralRules, {{name}} substitution and the message byte bound; the result
// is undefined when the template, its plural argument or a placeholder is unusable.
function renderMessage(template: ValidationTemplate, locale: string, args: Readonly<Record<string, string>>, plural?: string, pluralKind?: "cardinal" | "ordinal"): string | undefined {
  let text: unknown = template;
  if (plural) {
    if (typeof template !== "object" || template === null) return undefined;
    const number = args[plural];
    if (typeof number !== "string") return undefined;
    const parsed = Number(number);
    // Intl rounds fractional scale independently of the numeric round trip.
    if ((number.split(".")[1]?.length ?? 0) > maxPluralFractionDigits) return undefined;
    // Never silently round a wide declaration bound.
    if (!Number.isFinite(parsed) || compareNumbers(parsed, number) !== 0 || Math.abs(parsed) > Number.MAX_SAFE_INTEGER) return undefined;
    let form: ValidationPluralForm;
    try { form = new Intl.PluralRules(locale, { type: pluralKind ?? "cardinal", maximumFractionDigits: maxPluralFractionDigits }).select(parsed); } catch { return undefined; }
    text = Object.hasOwn(template, form) ? template[form] : template.other;
  }
  if (!messageText(text)) return undefined;
  const parts: string[] = []; let size = 0, offset = 0;
  const append = (part: string): boolean => {
    const bytes = textBytes(part); if (bytes > runtimePolicy.messageBytes - size) return false;
    size += bytes; parts.push(part); return true;
  };
  const tokens = /\{\{([^{}]*)\}\}|\{\{|\}\}/g;
  for (let token = tokens.exec(text); token; token = tokens.exec(text)) {
    const name = token[1];
    if (name === undefined || !Object.hasOwn(args, name) || !append(text.slice(offset, token.index)) || !append(args[name]!)) return undefined;
    offset = tokens.lastIndex;
  }
  return append(text.slice(offset)) ? parts.join("") : undefined;
}
const pluralForms: readonly string[] = ["zero", "one", "two", "few", "many", "other"];
/** catalogTranslations flattens one locale's catalog files, such as imported lang/<locale>/*.json modules, into the translations of validationMessages, formatText and formatMessage. The Go catalog rules apply: nested objects flatten with dots into semantic keys, {"$plural": {...}} leaves hold at most six known plural forms including other, texts are bounded, and no key is defined twice; a violation throws ContractError naming the file index and key. */
export function catalogTranslations(files: readonly unknown[]): Readonly<Record<string, ValidationTemplate>> {
  const result: Record<string, ValidationTemplate> = Object.create(null);
  let count = 0, bytes = 0;
  const fail = (index: number, key: string, code: string): never => reject(pointer("", String(index)) + (key ? pointer("", key) : ""), code);
  const add = (index: number, key: string, template: ValidationTemplate, size: number): void => {
    if (Object.hasOwn(result, key)) fail(index, key, "catalog_duplicate");
    count++; bytes += size;
    if (count > runtimePolicy.maxMessages || bytes > runtimePolicy.metadataBytes) fail(index, key, "catalog_bounds");
    result[key] = template;
  };
  const visit = (index: number, prefix: string, value: unknown, depth: number): void => {
    if (depth > 32 || typeof value !== "object" || value === null || Array.isArray(value)) fail(index, prefix, "catalog_object");
    for (const name of Object.keys(value as object).sort()) {
      const key = prefix ? prefix + "." + name : name, leaf: unknown = (value as Record<string, unknown>)[name];
      if (!semanticID(key)) fail(index, key, "catalog_key");
      if (typeof leaf === "string") {
        if (!messageText(leaf)) fail(index, key, "catalog_text");
        add(index, key, leaf, textBytes(key) + textBytes(leaf));
      } else if (typeof leaf === "object" && leaf !== null && !Array.isArray(leaf) && Object.hasOwn(leaf, "$plural")) {
        const forms: unknown = (leaf as Record<string, unknown>).$plural;
        if (Object.keys(leaf).length !== 1 || typeof forms !== "object" || forms === null || Array.isArray(forms) || !Object.hasOwn(forms, "other")) fail(index, key, "catalog_plural");
        const entries = Object.entries(forms as Record<string, unknown>), template: Record<string, string> = Object.create(null);
        let size = textBytes(key);
        if (entries.length > pluralForms.length) fail(index, key, "catalog_plural");
        for (const [form, text] of entries) {
          if (!pluralForms.includes(form) || !messageText(text)) fail(index, key, "catalog_plural");
          template[form] = text as string; size += textBytes(text as string);
        }
        add(index, key, Object.freeze(template) as ValidationTemplate, size);
      } else if (typeof leaf === "object" && leaf !== null && !Array.isArray(leaf)) {
        visit(index, key, leaf, depth + 1);
      } else fail(index, key, "catalog_leaf");
    }
  };
  files.forEach((file, index) => visit(index, "", file, 1));
  return Object.freeze(result);
}
/** formatText renders display text from catalog translations, such as a frontend-only key read with catalogTranslations, with the validation message renderer: {{name}} placeholders, the fallback catalog, Intl.PluralRules for options.plural and the text bound. A missing key or unrenderable text returns the key. */
export function formatText(messages: ValidationMessages | undefined, key: string, args: Readonly<Record<string, string | number | boolean>> = {}, options: { readonly plural?: string | undefined; readonly pluralKind?: "cardinal" | "ordinal" | undefined } = {}): string {
  const values: Record<string, string> = Object.create(null);
  for (const [name, value] of Object.entries(args)) {
    if (typeof value === "string") values[name] = value;
    else if (typeof value === "boolean") values[name] = value ? "true" : "false";
    else if (typeof value === "number" && Number.isFinite(value)) values[name] = String(value);
    else return key;
  }
  for (const catalog of [messages, messages?.fallback]) {
    if (!catalog || !Object.hasOwn(catalog.translations, key)) continue;
    const rendered = renderMessage(catalog.translations[key]!, catalog.locale, values, options.plural, options.pluralKind);
    if (rendered !== undefined) return rendered;
  }
  return key;
}
