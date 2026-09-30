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
void [values, quoted, limit, readers, issues, ranges, rangeBytes, numbers, rounded];
