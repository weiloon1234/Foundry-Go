import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const [sdk, members] = await Promise.all(process.argv.slice(2, 4).map(path => import(pathToFileURL(path).href)));
// One locale's JSON catalog files, as a bundler imports them.
const en = sdk.catalogTranslations([
  { cart: { items: { $plural: { one: "{{name}} has {{count}} item", other: "{{name}} has {{count}} items" } } }, welcome: "Hello {{name}}" },
  { auth: { failed: "Wrong {{identifier}} or password.", lockout: { $plural: { one: "Try again in {{minutes}} minute.", other: "Try again in {{minutes}} minutes." } } } },
  { position: { $plural: { one: "{{position}}st", two: "{{position}}nd", few: "{{position}}rd", other: "{{position}}th" } }, wire: { literals: "{{text}}={{enabled}}" } },
]);
const messages = { locale: "ms", translations: sdk.catalogTranslations([{ welcome: "Helo {{name}}" }]), fallback: { locale: "en", translations: en } };
assert.equal(sdk.formatMessage(messages, "welcome", { name: "Ada" }), "Helo Ada");
// The fallback catalog and the declaration's plural parameter and kind apply.
assert.equal(sdk.formatMessage(messages, "cart.items", { count: 1, name: "Ada" }), "Ada has 1 item");
assert.equal(sdk.formatMessage(messages, "cart.items", { count: "2", name: "Ada" }), "Ada has 2 items");
assert.equal(sdk.formatMessage(messages, "position", { position: 2 }), "2nd");
assert.equal(sdk.formatMessage(messages, "wire.literals", { enabled: true, text: "x" }), "x=true");
assert.equal(sdk.formatMessage(messages, "fields.name"), "fields.name");
// Frontend-only keys name their plural argument; unusable text returns the key.
assert.equal(sdk.formatText(messages, "auth.lockout", { minutes: 5 }, { plural: "minutes" }), "Try again in 5 minutes.");
assert.equal(sdk.formatText(messages, "auth.lockout", { minutes: 1 }, { plural: "minutes" }), "Try again in 1 minute.");
assert.equal(sdk.formatText(messages, "auth.lockout", { minutes: 5 }), "auth.lockout");
assert.equal(sdk.formatText(messages, "auth.failed", { identifier: "email" }), "Wrong email or password.");
assert.equal(sdk.formatText(messages, "auth.failed", {}), "auth.failed");
assert.equal(sdk.formatText(messages, "auth.failed", { identifier: Number.NaN }), "auth.failed");
assert.equal(sdk.formatText(undefined, "missing"), "missing");
assert.ok(Object.isFrozen(en) && Object.isFrozen(en["auth.lockout"]));
// The Go catalog rules reject what Load and NewCatalog would reject.
for (const files of [
  [{ Upper: "x" }], [{ a: 1 }], [{ a: [] }], ["text"], [null],
  [{ a: { $plural: { one: "x" } } }], [{ a: { $plural: { other: "x", many: 2 } } }], [{ a: { $plural: { other: "x", bogus: "y" } } }],
  [{ a: { $plural: { other: "x" }, b: "y" } }], [{ "a.b": "x" }, { a: { b: "y" } }], [{ a: "x\u0000" }],
]) assert.throws(() => sdk.catalogTranslations(files), sdk.ContractError);
// Surfaces keep every declared message and share the runtime renderer.
assert.equal(members.formatMessage(messages, "welcome", { name: "Ada" }), "Helo Ada");
assert.equal(members.formatText, sdk.formatText);
console.log("catalog messages ok");
