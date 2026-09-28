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
  const render = (template: ValidationTemplate, locale: string, plural = true): string | undefined => {
    let text: unknown = template;
    if (plural && recipe.definition.plural) {
      if (typeof template !== "object" || template === null) return undefined;
      const number = args[recipe.definition.plural]!, parsed = Number(number);
      // Intl rounds fractional scale independently of the numeric round trip.
      if ((number.split(".")[1]?.length ?? 0) > maxPluralFractionDigits) return undefined;
      // Never silently round a wide declaration bound.
      if (!Number.isFinite(parsed) || compareNumbers(parsed, number) !== 0 || Math.abs(parsed) > Number.MAX_SAFE_INTEGER) return undefined;
      let form: ValidationPluralForm;
      try { form = new Intl.PluralRules(locale, { type: recipe.definition.plural_kind ?? "cardinal", maximumFractionDigits: maxPluralFractionDigits }).select(parsed); } catch { return undefined; }
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
  };
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
