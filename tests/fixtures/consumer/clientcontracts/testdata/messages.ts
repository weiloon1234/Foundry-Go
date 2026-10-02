import { catalogLocales, formatMessage, formatText, type CatalogMessageKey, type Locale, type ValidationMessages } from "./contracts_foundry.gen.js";

declare const messages: ValidationMessages;
export function messageConsumer(): void {
  void formatMessage(messages, "cart.items", { count: 2, name: "Ada" });
  void formatMessage(messages, "cart.items", { count: "2.5", name: "Ada" });
  void formatMessage(messages, "fields.name");
  // @ts-expect-error A declared message requires every typed argument.
  void formatMessage(messages, "cart.items", { count: 2 });
  // @ts-expect-error A misspelled key is not a declared message.
  void formatMessage(messages, "cart.itemz", { count: 2, name: "Ada" });
  // @ts-expect-error A boolean parameter is not text.
  void formatMessage(messages, "wire.literals", { enabled: "yes", text: "x" });
  // Frontend-only keys name their plural argument explicitly.
  void formatText(messages, "auth.lockout", { minutes: 5 }, { plural: "minutes" });
  const key: CatalogMessageKey = "welcome";
  void key;
  // The server's configured locales, typed as the catalog's Locale union.
  const fallback: Locale | undefined = catalogLocales?.default;
  const supported: readonly Locale[] = catalogLocales?.supported ?? [];
  void fallback; void supported;
}
