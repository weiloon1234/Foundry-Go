# Request-aware validation messages

Built-in rules now carry a message key, typed declaration arguments and an English
fallback. HTTP renders them with the selected request locale. This changes human
text; status, issue code, JSON Pointer, order and truncation remain stable. Decoding
still returns 400 and rule rejection returns 422. Infrastructure failures retain
safe 500 responses.

## Configured applications

Enable `settings.Features.Locales.Enabled` and configure its `Default`, `Locales`
and optional `Fallback`. The HTTP kernel automatically applies `http.Locale` and
registers `validation.MessageDefinitions()` and `http.MessageDefinitions()`.
Supply translations through `application.FeatureDeclarations.Catalog`; only your
own additional message signatures go in `Messages`.

```go
Catalog: map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{
    "ms": {
        "validation.min_length": {
            Forms: map[i18n.PluralForm]string{
                i18n.Other: "{{attribute}} mesti mempunyai sekurang-kurangnya {{min}} aksara.",
            },
        },
        "http.error.validation_failed": {Text: "Pengesahan gagal"},
    },
},
```

Built-in validation keys replace the `foundry.` rule prefix with `validation.`.
Every rule accepts a text `attribute`. Length/item bounds add numeric `min` or
`max`; file size adds numeric `bytes`; `validation.decimal_places` adds numeric
`places`. These select plural forms. Numeric bounds and divisors preserve their
exact declared text. Comparison/date bounds use text `other`, and
`validation.date_format` uses text `format`. Relative time rules have
parameter-free keys (`validation.after_now`, `after_or_equal_now`, `before_now`,
`before_or_equal_now`, `after_today`, `after_or_equal_today`, `before_today`,
`before_or_equal_today`), as do `validation.image_dimensions` and
`validation.unique_all`. Inspect `validation.MessageDefinitions()` for the
complete signatures.

`Dynamic` rules and `Hook` reports may choose a message at check time with
`validation.RejectWith(generatedMessage, args)`; it renders in the request locale
with the rule's declared message as English fallback, and its `attribute`/`other`
arguments must be text like other validation messages.
HTTP envelope keys are `http.error.<error_code>`; parameter-free decoding keys are
`http.input.type`, `key`, `null`, `required`, `unknown`, `value` and `length`.

A generated field's `WithLabel` supplies its public name; `WithLabelKey` selects
a parameter-free catalog message with that static name as fallback. Unlabeled
fields use their wire name in message text. Comparisons carry both field labels;
scalar collection items keep their parent label and original index paths.

Explicit request locale wins, followed by `Accept-Language` and the configured
default. Catalog lookup tries that locale, then configured fallback, then the
rule's English text, even when English is not enabled. Error responses include
`Content-Language` for the selected audience and `Vary: Accept-Language`, alongside
`Cache-Control: no-store`. Individual missing messages can still contain English.
Successful responses retain their own language/cache policy.

## Typed overrides and non-HTTP use

`WithMessage` is literal and wins over translations, including `{{braces}}`.
`validation.WithTranslation(rule, GeneratedMessage(), Args{...})` attaches the
existing generated `message.Message[Args]` contract. Go checks the argument owner
and preserves the rule's value type. These arguments are approved public
**declaration data**, captured once; never pass submitted passwords or other
secrets. Optional text `attribute` and `other` arguments receive bound labels.
Without a bound field, explicitly supplied arguments are preserved.
The rule's current English message remains the literal fallback. Call
`WithMessage` first to customize that fallback, or last for an unconditional literal.

See the executable [typed override](../../tests/fixtures/consumer/localization/validation.go)
and [configured HTTP acceptance](../../tests/fixtures/consumer/configuredprofile/localization_test.go).
Apply an override to a generated field's `.Rules(...)` for field-specific wording.
Custom rules use exactly the same `WithTranslation` function.

Direct `Rule.Check` returns English diagnostics. CLI/worker callers can explicitly
use `rejected.Localize(ctx, catalog, locale)` to obtain an owned translated copy;
`LocalizeLabels` continues to change labels only. Rules, errors and catalogs can
be shared across concurrent locale requests without mutation.

For direct router assembly, include framework message definitions when creating
the catalog and apply `http.Locale(catalog)`. A directly wrapped router and the
configured HTTP kernel validate registered signatures before serving. When hiding
a router behind custom middleware, call `validation.ValidateMessages` on each
endpoint's validation description yourself. Missing optional signatures are fine;
registered conflicting signatures or parameterized field labels are errors.
Unknown placeholders and malformed templates fail catalog construction. Runtime
presentation failures retain safe English rejection diagnostics.

## Generated browser validation

`Spec.Translation` exports the same owned recipe used by Go. The generated client
renders its English fallback, including labels and bounds. Pass
`ClientOptions.validationMessages` with `{locale, translations, fallback?}` to
translate browser rule diagnostics. Templates use the same `{{argument}}` placeholders
and plural form names; labels are parameter-free strings. Invalid/missing client
translations use exported English text. `Intl.PluralRules` handles supported
numeric arguments; bounds beyond JavaScript's exact integer range or with more
than 20 fractional digits use English rather than rounding. Go catalog rendering
continues to select plural forms using exact decimal arguments. Render text with
the escaping required by your UI.

Browser localization does not execute unsupported rules: server-only rules remain
in `skipped` and `complete` stays false. The HTTP server remains authoritative. Strict client wire/codec errors retain their existing code-based diagnostics.

Regenerate exported frontend adapters with the same framework version. Rule
metadata now includes an additive `translation` field; older strict manifest
readers may reject it. The HTTP error DTO shape is unchanged, while human message
text deliberately becomes label-aware and locale-dependent.

## Acceptance

The final native `make verify` passed with required PostgreSQL, Redis, TypeScript
and real gopls checks, including independent consumers, compiler negatives and
current generation. Relevant framework and consumer races passed. The review
also tightened client output bounds and preserved explicit unbound message
arguments. See the [acceptance record](../evidence/validation-messages-20260926.json).
