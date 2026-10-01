import * as sdk from "./contracts_foundry.gen.js";
const choice = sdk.schema("client.ExactEnum");
const values: readonly "9223372036854775807"[] = choice.choices;
const quoted: readonly "9223372036854775807"[] = sdk.schema("client.QuotedExactEnum").choices;
const limit: sdk.ExactMetadataNumber = sdk.operation("itemsEcho").metadata.limits.Body.Bytes;
const readers: sdk.ExactMetadataNumber = sdk.operation("uploadsProfile").metadata.limits.Multipart.Readers;
const issues: sdk.ExactMetadataNumber = sdk.operation("uploadsProfile").metadata.limits.Multipart.Issues;
const ranges: sdk.ExactMetadataNumber = sdk.operation("itemsEcho").metadata.limits.Files.Ranges;
const rangeBytes: sdk.ExactMetadataNumber = sdk.operation("itemsEcho").metadata.limits.Files.RangeBytes;
// @ts-expect-error Exact enum choices are not floating-point numbers.
const numbers: readonly number[] = choice.choices;
// @ts-expect-error Unsafe-size limits must be inspected before numeric use.
const rounded: number = sdk.operation("itemsEcho").metadata.limits.Body.Bytes;
// Maps keyed by model IDs are collections: entries bind by identity, never as fields.
const index = sdk.schema("foundry.test/consumer/clientcontracts.OwnerIndex");
const owners = index.field("owners");
declare const user: Parameters<typeof owners.at>[0];
const ownerEntry = owners.at(user), ownerTemplate = owners.element();
const ownerValue: string = null as unknown as sdk.FieldValue<typeof ownerEntry>;
// @ts-expect-error Identity keys are entries, not declared fields.
owners.field(user);
// @ts-expect-error A model-ID key is an Identity, not an arbitrary string.
owners.at("not-an-identity");
const second = index.field("pair").at(1), state = index.field("states").field("draft");
// variant() takes a union's tag, not an enum-typed property of a plain object.
const card = sdk.operation("unionsEcho").field("body", "method").variant("kind", "card").field("token");
// @ts-expect-error Payload is not a union; its enum-typed state is not a discriminator.
sdk.schema("foundry.test/consumer/clientcontracts.Payload").variant("state", "draft");
// A LocaleMap is a collection of supported locales: entries bind with at().
const titles = sdk.operation("itemsEcho").field("body", "titles"), title = titles.at("ms"), anyTitle = titles.element();
// @ts-expect-error Locale keys are entries, not declared fields.
titles.field("en");
// @ts-expect-error Only supported locales are request keys.
titles.at("fr");
// A JSON body root is described through body(); a named-part body has no JSON root.
const rootCard = sdk.operation("unionsMethod").body().variant("kind", "card").field("token");
// @ts-expect-error Multipart and form bodies have no JSON root to describe.
sdk.operation("uploadsProfile").body();
void [values, quoted, limit, readers, issues, ranges, rangeBytes, numbers, rounded, ownerTemplate, ownerValue, second, state, card, rootCard, title, anyTitle];
