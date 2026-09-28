# 20 — Localization and supporting APIs

## Purpose and prerequisites

Prerequisites: [02](02-foundation-and-application-lifecycle.md), [03](03-generation-and-language-tooling.md), [08](08-http-validation-and-responses.md), [09](09-redis-cache-and-coordination.md). Complete Rust's supporting capabilities with focused Go packages and shared contracts.

Rust references: `src/i18n`, `src/app_enum`, `src/support`, `src/http_client`; `tests/http_client_acceptance.rs`, `support_stores_acceptance.rs`; `docs/guides/i18n.md`, `http-client.md`.

## Shared prerequisite introduced by milestone 10

The `encryption` package is now written for MFA's encrypted factor secrets, with
versioned AES-256-GCM, record-bound authentication and retained-key rotation.
It passed milestone 10's gate. Reuse this implementation when finishing
supporting cryptographic APIs; do not introduce another library for the same
function. See [the guide](../docs/guides/encryption.md). This does not advance the
milestone 20 completion status or complete all supporting APIs.

## Public boundaries

Milestone 18 introduces `i18n.LocaleID`, immutable `LocaleSet` and the injected
`LocaleCatalog` snapshot contract as prerequisites for model translations and
localized attachments. Reuse that contract for this milestone's catalogs and
request resolution; do not create a second locale ID or supported-locale list.

- `i18n` owns locale catalogs, request resolution, fallback, message formatting and pluralization. Configuration owns supported/default locales.
- The generator emits typed translation keys/parameter descriptors and enum metadata from the existing sources; no second list of enum values or locale configuration is maintained for clients.
- `httpclient` owns named client configuration, reusable standard transports, deadlines, retries, streaming, fakes and typed decoding.
- Collection, cryptographic, sanitization and temporal helpers remain focused packages; do not create a catch-all mutable `utils` service.

Delivered localization shape:

```go
result, err := messages.WelcomeArgsMessage().Format(ctx, catalog, locale,
    messages.WelcomeArgs{Name: name})
```

Generated keys/argument types are the discoverable path. Dynamic catalog keys are an explicit boundary for applications whose catalogs are not known at generation time.

The implementation uses a `//foundry:message` annotation on a Go argument struct,
reusing the DTO graph and emitter. Go methods cannot introduce their own type
parameters, so the generated `Message[Args]` receiver retains argument typing.
The base locale/catalog package stays independent from enum/contract/validation;
`i18n/message` owns the typed contract adapter. UI fallback is explicit and does
not change the existing model-content fallback policy. See the
[localization guide](../docs/guides/localization.md) and
[supporting APIs guide](../docs/guides/supporting-apis.md). Implementation and
consumer review passed the native milestone acceptance gate.

The [outbound HTTP guide](../docs/guides/http-client.md) records the concrete named
client, provider, body/retry, typed codec, streaming and bounded fake contracts.
One private native transport constructor is shared with email adapters without
changing their one-attempt delivery policy.

## Implementation slices

1. Catalog loading, locale normalization/resolution, configured fallback, plural forms, interpolation and typed generator integration.
2. Validation/enum/permission labels and shared client metadata; integrate the locale contract used by model translations.
3. Outbound HTTP client factories, request/response codecs, cancellation, connection reuse, bounded retries and test transports.
4. Reuse existing hashing/signing primitives; complete encryption/key rotation, token utilities and HTML sanitization using approved established implementations.
5. Complete collection and temporal helpers needed for Rust parity while retaining normal Go slices, maps, iterators and time interoperability.

## Failure behavior

Missing translations have an explicit fallback/diagnostic policy and must not crash unrelated requests. Invalid locales do not bypass the supported-locale set. Keep model content translations separate from UI/message catalogs.

HTTP retries require replayable bodies and an operation policy safe for retries. Do not retry arbitrary POST side effects automatically. Bound response decoding and close bodies on every path. Credential-bearing headers and query data are redacted from diagnostics.

Cryptographic formats are versioned and use authenticated encryption and approved primitives. Rotation supports reading old keys during a configured transition; failures do not return plaintext or expose key material.

## Acceptance

Test parameter typing, missing keys, pluralization, locale fallback, concurrent requests with different locales, enum label/contract consistency, HTTP cancellation/retries/body replay, connection cleanup, encrypted-format compatibility and sanitization attack inputs. Apply the [common gate](README.md#common-completion-gate).


## Delivered behavior and verification

Immutable bounded catalogs, generated typed argument descriptors, requested/default
fallback, CLDR cardinal/ordinal selection, explicit request contexts and shared
feature label metadata passed milestone 20. Named outbound clients own connection
pools, bounded operations, retries and streaming lifetime, with existing generated
JSON contracts and deterministic fakes. Cryptographic helpers reuse established
implementations; passive HTML sanitization uses bluemonday, and collection/temporal
helpers retain ordinary Go interoperability. The
[recorded evidence](00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
includes fresh generation, consumer/compiler/editor, race, fuzz, compatibility and
full native regression checks, plus the verification corrections and limits.
