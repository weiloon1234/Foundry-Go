# Localization

See [request-aware validation messages](validation-messages.md) for automatic HTTP
rendering, built-in translations, typed overrides and generated client presentation.

Milestone 20 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
records the checks and operational limits.

Use one `i18n.LocaleSet` from application configuration for UI catalogs, model
translations and localized attachments. Translated model fields declared as
[model extension slots](model-extension-slots.md) follow the same catalog, and
`validation.Locales` checks locale-keyed request input against it. `i18n.Catalog` implements that same
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

The message signature and per-parameter wire metadata are resolved once when the
generated message is defined. `Format` encodes arguments through the generated
JSON descriptor, which remains the single source of wire names, custom codecs and
quoting. For repeated rendering of the same arguments, `Bind` once and call
`PreparedMessage.Format`: it checks the catalog signature without copying and
renders already validated arguments. Plural rules use language tags parsed once
per catalog locale.

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

Entries whose names start with a dot (`.DS_Store`, `.git`, `.gitkeep`) are ignored
at every level without being opened, as are regular files without a `.json`
suffix (`README.md`, editor backups). A `.json` file at the catalog root, nested
directories inside a locale, symlinks, unknown keys, unknown locales, duplicate
JSON keys, duplicate flattened keys, duplicates across files, locale aliases that
collide, malformed leaves and placeholders fail loading. Resource limits are 64
locales, 1,024 catalog files, 4,096 directory entries per directory, 1 MiB per
file, 16 MiB aggregate catalog data, 10,000 message declarations, 64 parameters
and 64 KiB per template/result. Inputs and metadata are copied.

Every load and catalog error is `fault.Invalid` and names the locale, file and
key where known, with a fixed reason, for example
`invalid localization catalog (locale "ms", file "ms/cart.json", key "cart.items"): placeholder "nmae" is not a declared parameter`.
A duplicate key names the file that first defined it. Coordinates are quoted as
bounded ASCII; template text, JSON values and argument values are never included.
Declaration and argument errors similarly name the message key and parameter.
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
use `http.Locale(catalog)` with the existing middleware API, or
`http.LocaleWith(catalog, http.LocaleNegotiation{QueryParameter: "lang", Cookie: "locale", Preferred: userLocale})`
to consult an explicit query parameter, a locale cookie and an application
preference (such as the authenticated user's stored locale) before the context
locale and Accept-Language. The query selector is request metadata: typed
endpoints ignore it unless they declare a parameter of the same name, and a
signed link still verifies when it is appended. Each value is matched against the catalog; malformed
or unsupported values defer to the next source, while a `Preferred` error is
returned as the shared error response. An authenticated preference needs the
actor, so install `LocaleWith` on the authenticated route or scope, inside its
authentication middleware; there it overrides a global `Locale`. Response cache and
`Vary: Accept-Language` policy remain explicit because the response may also depend
on a user's stored preference. The framework's `LocaleSet` and `*Catalog` run no
application code, so `WithLocale` and `SnapshotLocales` read them directly without
an isolation goroutine; other `LocaleCatalog` implementations remain isolated.

`LocaleSet.Match(id)` is the one parent-locale rule: it returns `id` when
supported, otherwise its nearest supported parent tag (`en-GB` → `en`,
`zh-Hant-TW` → `zh-Hant`), never an unrelated sibling. As in CLDR, a script
subtag directly after the language ends the chain: `zh-Hant` does not fall back
to `zh`, nor `sr-Latn` to `sr`, because the parent may use another writing
system; such requests continue with the default. `MatchTag` parses
external text first, and `MatchAcceptLanguage` applies the header rules above.

UI lookup tries the requested locale, its supported parents, then the configured
`CatalogOptions.Fallback` (the default locale when omitted). With `en-GB`, `en`
and default `ms` supported, an `en-GB` request uses an `en-GB` translation, then
`en`, then `ms`. It does not try arbitrary other supported locales.
`Result.Fallback` identifies a fallback translation. When a registered message is
absent in the whole chain, `Result.Missing` is true and `Text` contains its key.
This is a successful, diagnosable missing translation; it does not log argument
values. An undeclared dynamic key or incompatible arguments is a configuration
error. Model-content `LocaleSet.Fallbacks` uses the same parents: requested,
supported parents, the default, then the remaining supported locales in lexical
order.

## Locale preferences

`i18n.LocaleResolver[S]` selects a locale for a typed subject — an HTTP request,
an authenticated user or a notification recipient — from ordered steps, then the
catalog default:

```go
resolver, err := i18n.NewLocaleResolver(catalog,
    i18n.ContextLocale[*Recipient](),
    i18n.Preferred("user", func(ctx context.Context, r *Recipient) (i18n.LocaleID, bool, error) {
        return r.Locale, r.Locale != "", nil // a stored, canonical preference
    }),
    i18n.AcceptLanguage(func(r *Recipient) string { return r.AcceptLanguage }),
)
resolution, err := resolver.Resolve(ctx, recipient)
// resolution.Locale is supported; resolution.Source is "context", "user",
// "accept_language" or "default".
ctx, resolution, err = resolver.WithResolvedLocale(ctx, recipient)
```

`ContextLocale` uses a locale recorded by `WithLocale`. `Preferred` adapts an
explicit or stored preference under an application source name (a semantic
identifier; `context`, `accept_language` and `default` are reserved). A stored
locale that is no longer supported falls back to its nearest supported parent or
defers to the next step. A malformed stored ID such as `en_US` is treated as no
preference: the step defers and `Resolution.Ignored` names the first such step,
so one bad profile value never breaks that user's requests. Normalize input with
`ParseLocale` before storing it. `AcceptLanguage` reads a header value from
the subject with the bounds and quality rules above. Mail and notification
delivery typically use a recipient preference without a header step.

Every call takes one supported-locale snapshot. Lookup failures, contained
panics and cancellation return an error instead of silently selecting the
default. Lookups run as application callbacks with panic containment but no
goroutine, must honor `ctx` and be concurrency-safe. A resolver accepts at most
16 distinct steps, is immutable and can be shared across requests and workers.

Dynamic applications explicitly supply `i18n.MessageDefinition` and call
`Catalog.FormatDynamic` with typed `Text`, `Number` and `Boolean` arguments. They
receive runtime signature checks rather than generated Go argument typing.

## Feature labels

- `//foundry:enum labels=enum.account.status` emits a label key for each actual
  constant, using its snake-case Go name. Unannotated enum output remains unchanged.
  `Descriptor.LabelDefinitions()` contributes parameter-free catalog entries once
  per distinct key, so explicit cases can share a label;
  `Definition().Cases` includes the same labels and exact wire values. `Label` and
  `LabelKey` reject unknown enum values. A descriptor validates once per
  `Describe` result; retain it for repeated label lookups.
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
