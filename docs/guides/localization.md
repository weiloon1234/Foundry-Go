# Localization

See [request-aware validation messages](validation-messages.md) for automatic HTTP
rendering, built-in translations, typed overrides and generated client presentation.

Milestone 20 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
records the checks and operational limits.

Use one `i18n.LocaleSet` from application configuration for UI catalogs, model
translations and localized attachments. `i18n.Catalog` implements that same
`LocaleCatalog` interface. It is immutable, creates no background work and can be
injected with an ordinary foundation factory. Each operation receives an explicit
locale; there is no process-wide current locale.

## Declare typed messages

The Go argument struct is the source of truth for parameter names and types:

```go
//foundry:message key=cart.items plural=count
type CartArgs struct {
    Name  string          `json:"name"`
    Count decimal.Decimal `json:"count"`
}
```

`foundry generate` emits `CartArgsMessage()`, `CartArgsJSON()` and
`CartArgsValidationFields()` in one owned file. `CartArgsMessage().Format` accepts
only `CartArgs`. It derives parameter metadata from the generated JSON graph;
there is no separate hand-maintained argument schema. Arguments must be required,
nonnullable scalar text, booleans, integers or exact decimals. Optional values,
floats, nested objects and custom JSON-contract parameters are rejected by
generation. Enum parameters retain their existing wire contract. `json:",string"`
is supported through the existing DTO codec rules.

```go
result, err := CartArgsMessage().Format(ctx, catalog, locale, CartArgs{
    Name: "Ada", Count: decimal.FromInt64(2),
})
```

`Message.Key()` supplies the shared typed `i18n.MessageKey`. `Definition()` and
`Description()` return owned message/argument metadata for contract exporters.
`Registration()` erases the argument type only during catalog assembly. Formatting
still checks the catalog's complete parameter and plural signature.

## Load catalogs

Pass `os.DirFS` or a scoped `embed.FS` containing locale directories such as
`en/messages.json` and `ms/messages.json`. Loading is explicit and all-or-nothing.
One catalog can merge several files per locale. Directories normalize through the
existing BCP 47 locale parser and must belong to the configured supported set.

```json
{
  "cart": {
    "items": {
      "$plural": {
        "one": "{{name}} has one item",
        "other": "{{name}} has {{count}} items"
      }
    }
  }
}
```

```go
catalog, err := message.Load(ctx, files, locales, i18n.CatalogOptions{},
    CartArgsMessage().Registration(),
)
```

Nested objects flatten with dots. Plain leaves are strings; plural leaves contain
only `$plural`. A plural declaration names its numeric Go parameter. `kind=ordinal`
selects ordinal rules; the default is cardinal. Forms are CLDR `zero`, `one`, `two`,
`few`, `many`, `other`; `other` is required and supplies an omitted category.
Rules use the locale of the selected translation, including fallback translations.
Decimal operands retain exact numeric value and use `decimal.Decimal`'s canonical
scale: `1.0` and `1` have the same plural semantics.

Rules come from the installed `golang.org/x/text/feature/plural` CLDR tables;
its `CLDRVersion` reports their version. Large integer/fraction operands retain
their rule-relevant digits through the package's
[operand API](https://pkg.go.dev/golang.org/x/text/feature/plural#Rules.MatchPlural),
without floating-point conversion or treating a large value as a small literal.

Templates accept only exact `{{parameter}}` placeholders from the declaration.
Substitution occurs once, so argument text containing braces is preserved. Output
is plain text and must be escaped for the destination HTML, URL or other context.
No expressions, functions or HTML trust are inferred from a translation.

Unknown keys, unknown locales, duplicate JSON keys, duplicate flattened keys,
duplicates across files, locale aliases that collide, symlinks, malformed leaves
and placeholders fail loading. Resource limits are 64 locales, 1,024 files,
1 MiB per file, 16 MiB aggregate catalog data, 10,000 message declarations,
64 parameters and 64 KiB per template/result. Inputs and metadata are copied.
Filesystem callbacks are isolated and awaited through actual return. Directory
handles must implement `fs.ReadDirFile` so enumeration can be bounded; `embed.FS`,
`os.DirFS` and `os.Root.FS` support this. Choose `os.Root.FS` when the filesystem
root itself needs confinement against concurrent path replacement.

## Resolution and fallback

`Catalog.Resolve(preferred, acceptLanguage)` first considers an explicit locale,
then quality-sorted language candidates, then the configured default. Supported
parent tags can satisfy regional or extension tags; unrelated sibling locales
are not chosen. Malformed and zero-quality candidates are ignored. The header is
bounded to 8 KiB and 64 candidates. This is locale selection, not HTTP 406
negotiation: when none matches, the default is used.

`i18n.WithLocale(ctx, catalog, locale)` accepts only supported IDs.
`i18n.RequestLocale(ctx)` reads that request-local selection. HTTP applications can
use `http.Locale(catalog)` with the existing middleware API. Response cache and
`Vary: Accept-Language` policy remain explicit because the response may also depend
on a user's stored preference.

UI lookup tries the requested locale and the configured `CatalogOptions.Fallback`
(the default locale when omitted). It does not try arbitrary other supported
locales. `Result.Fallback` identifies a fallback translation. When a registered
message is absent in both catalogs, `Result.Missing` is true and `Text` contains its
key. This is a successful, diagnosable missing translation; it does not log argument
values. An undeclared dynamic key or incompatible arguments is a configuration
error. Model-content `LocaleSet.Fallbacks` retains its separate existing behavior.

Dynamic applications explicitly supply `i18n.MessageDefinition` and call
`Catalog.FormatDynamic` with typed `Text`, `Number` and `Boolean` arguments. They
receive runtime signature checks rather than generated Go argument typing.

## Feature labels

- `//foundry:enum labels=enum.account.status` emits a label key for each actual
  constant, using its snake-case Go name. Unannotated enum output remains unchanged.
  `Descriptor.LabelDefinitions()` contributes parameter-free catalog entries once
  per distinct key, so explicit cases can share a label;
  `Definition().Cases` includes the same labels and exact wire values. `Label` and
  `LabelKey` reject unknown enum values.
- Generated validation fields support `WithLabelKey(message.Key())`. Descriptions
  and issues retain that key separately from the JSON Pointer. `Errors.LocalizeLabels`
  returns copied diagnostics and keeps the static label if its registered
  translation is missing. That method leaves rule messages unchanged; `Errors.Localize` also renders them from shared recipes.
- `Permission.WithLabelKey` contributes `PermissionDescription` metadata while
  preserving its exact registered policy identity and fresh authorization behavior.
  A label never grants a permission.
- Datatable dependencies can use the catalog as `Locales` and `catalog.Label` as
  `Labels`. Exports keep their explicitly recorded locale.

The independent [localization consumer](../../tests/fixtures/consumer/localization/messages.go)
contains generated-message, enum, validation, permission and concurrent HTTP usage.
