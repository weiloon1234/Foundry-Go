import {
  catalogLocales,
  decodePublicEnv,
  formatFrontendMessage,
  formatMessage,
  formatText,
  type CatalogMessageKey,
  type FrontendMessageKey,
  type Locale,
  type PublicEnv,
  type ValidationMessages,
} from "./contracts_foundry.gen.js";

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

  // S9: frontend message formatting and key checking
  void formatFrontendMessage(messages, "ui.welcome", { name: "Ada" });
  void formatFrontendMessage(messages, "ui.farewell");
  void formatFrontendMessage(messages, "auth.notice", { attempts: 3 });
  const feKey: FrontendMessageKey = "ui.welcome";
  void feKey;
  // @ts-expect-error A misspelled frontend key is not a declared message.
  void formatFrontendMessage(messages, "ui.unknown");
  // @ts-expect-error Missing required parameter fails compilation.
  void formatFrontendMessage(messages, "ui.welcome");
  // @ts-expect-error Parameter with missing required field fails compilation.
  void formatFrontendMessage(messages, "ui.welcome", {});
  // @ts-expect-error Non-existent key is not part of FrontendMessageKey union.
  const badFeKey: FrontendMessageKey = "ui.nonexistent";
  void badFeKey;

  // S6: public configuration decoding and typing
  const env: PublicEnv = decodePublicEnv({ VITE_APP_NAME: "Foundry Consumer", VITE_DEFAULT_LOCALE: "ms" });
  const appName: string = env.appName;
  const defaultLocale: Locale = env.defaultLocale;
  void [appName, defaultLocale];
  // @ts-expect-error Undeclared public config property is rejected.
  void env.undeclaredSetting;
  // @ts-expect-error Public config property has typed kind (Locale, not number).
  const wrongLocaleType: number = env.defaultLocale;
  void wrongLocaleType;
}
