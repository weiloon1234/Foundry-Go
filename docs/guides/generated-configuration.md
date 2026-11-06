# Generated typed deployment configuration

Consumer-startup C01 passed native full verification, configuration/generator
races and independent compiler/editor acceptance. It adds configuration
descriptors to the existing Go generator. Verification status is owned by the
[master](../../blueprint/00-master-architecture-and-parity.md#consumer-startup-delivery).
This feature derives the configuration schema consumed by [configured application assembly](application-bootstrap.md). The application registers domain declarations and constructors; the builder assembles the selected services.

Declare an exported Go settings struct with `//foundry:config`. Concrete nested
structs form groups. `config:"name"` selects the external name; absent/empty names
use snake case. Field names remain ordinary autocompletable Go fields.

```go
//foundry:config
type Settings struct {
    HTTP struct {
        Listen string
        Timeout time.Duration
    } `config:"http"`
    SigningKey secret.String `config:"signing_key"`
}
```

Original leading and trailing field comments also appear on generated key
fields, including nested groups and imported settings declarations. Completion
and hover therefore retain deployment guidance such as default connection and
queue selection. Edit that help on the original Go field, then regenerate. This
Go documentation does not change the configuration schema or runtime defaults.

Run `foundry generate --dir ./path/to/settings`; use `--check` in verification.
The generator uses its existing ownership manifest, atomic publication, complete
Go type check and recovery protocol. Do not hand-edit generated files.

It emits `SettingsConfigSchema()` and `SettingsConfigKeys()`. The latter includes
`keys.HTTP.Timeout`, whose `Set` accepts `time.Duration` and returns an override
owned by `Settings`. The generated schema uses the existing loader:

```go
schema, err := SettingsConfigSchema()
// Return err before loading if schema construction failed.
keys := SettingsConfigKeys()
settings, report, err := toml.LoadFile(
    "config/app.toml", schema, Defaults(),
    config.Inputs[Settings]{
        Environment: os.LookupEnv,
        Environ: os.Environ, // optional: per-entry named-collection overrides
        Prefix: "APP",
        Overrides: []config.Override[Settings]{
            keys.HTTP.Timeout.Set(5 * time.Second),
        },
        Validate: Validate,
    },
    toml.Options{},
)
```

`Defaults` and `Validate` are ordinary application Go functions. There is no
second handwritten schema or string-encoded business validation. Precedence is
defaults, file layers, optional [env file](#explicit-dotenv-input), environment,
typed overrides. `APP__HTTP__TIMEOUT` targets
`http.timeout`. `APP__HTTP__TIMEOUT_FILE` instead names a regular file (such as a
mounted secret) whose content is the value: at most 1 MiB, with one trailing
newline removed. Setting both names is an error, and declarations whose
environment names differ only by a `_FILE` suffix are rejected. Provenance
reports `environment-file:APP__HTTP__TIMEOUT` without the path or value.
File loading owns close, bounds input and uses the safe default
source label `configuration`. It requires a regular operator-controlled file;
it does not load `.env` implicitly or promise cancellation of arbitrary filesystem
I/O. Supply `Options.Name` only with a trusted credential-free label.

Named scalars preserve width; floats must be finite. Duration uses Go duration
text, and `secret.String` remains redacted and marks provenance as sensitive.
Generated enum membership is checked for file/env values, defaults and overrides.
Text decoding uses a concrete pointer implementing `encoding.TextUnmarshaler`.

Slices, arrays, string-keyed maps and `config:"name,json"` fields use the existing
strict JSON codec. TOML arrays/tables are normalized to its representation;
environment values use JSON text. Unknown struct fields fail. Custom codecs and
JSON duration representation follow the concrete Go type; ordinary
`time.Duration` inside an opaque JSON object is an integer number of nanoseconds,
not a duration string. A normal nested config group instead exposes duration text.
Interfaces and unsupported dynamic shapes are rejected during generation.

`config:"name,secret"` marks a field or every nested group leaf sensitive. An
opaque JSON field containing `secret.String` is sensitive as a whole.
`config:"-"` excludes input binding; it does not exempt the complete Go settings
value from the existing safe snapshot/copy requirements. Keep callbacks and
resource handles outside deployment settings.

Unknown fields, conflicting names/namespaces, malformed configuration and
validation failures return no partial settings or provenance. Formatted loader
errors omit input values; retained parser/I/O causes may include them and must
not be logged. See the [configuration consumer](../../tests/fixtures/consumer/startupconfig/settings.go)
and its [source precedence tests](../../tests/fixtures/consumer/startupconfig/settings_test.go).

Named infrastructure tables reuse these generated schemas; see [named services](named-services.md).

## Explicit dotenv input

Set `config.Inputs[Settings].EnvFile` to load one explicitly selected dotenv
document through either `schema.Load` or `toml.LoadFile`:

```go
inputs := config.Inputs[Settings]{
    EnvFile: config.EnvFile{Path: envPath, Name: "local.env"},
    Environment: os.LookupEnv,
    Overrides: overrides,
}
settings, report, err := schema.Load(Defaults(), inputs)
// With TOML, pass the same inputs to toml.LoadFile instead.
```

An empty `Path` disables the input. The loader does not search for `.env`, mutate
the process environment, or implicitly use `os.LookupEnv`. A nil `Environment`
uses only the env file for environment bindings; inject an empty lookup for the
same behavior in application loaders that default to `os.LookupEnv`.

The precedence is **defaults < TOML/file layers < env file < process environment
< typed overrides**. Presence is tested per variable, so a process variable set
to an empty string wins over the same env-file variable. The selected text goes
through the generated schema's existing decoders and validation. As with process
environment input, unrelated keys are ignored. The complete dotenv document is
still checked for syntax and duplicate keys, including unused or overridden keys.
`Prefix` applies normally. Dotenv supplies whole-setting environment bindings;
per-entry named-collection overrides still require the separate `Environ` input.

The adapter requires a regular file, checks the opened descriptor, reads at most
1 MiB plus one byte to detect overflow, and closes the file before decoding
settings. Files larger than 1 MiB, invalid UTF-8, NUL bytes and duplicate keys fail.
The grammar is deliberately literal:

- Each nonblank, noncomment line is `KEY=VALUE`. Keys match
  `[A-Za-z_][A-Za-z0-9_]*`; spaces and tabs around the key and value are allowed.
  LF and CRLF line endings are accepted.
- A line starting with `#` after whitespace is a comment. In an unquoted value,
  `#` starts a comment only at the start or after a space or tab; `a#b` is literal.
- Single quotes preserve their contents literally. Double quotes recognize only
  `\"` and `\\` escapes. Quoted values may contain leading/trailing spaces and `#`.
  After a closing quote, only whitespace and an optional comment are allowed;
  a comment must be separated from the quote by whitespace.
- Empty values are set values. Multiline quotes, bare carriage returns in values,
  `export KEY=...` and other escapes such as `\n` are rejected. `$NAME`, `${NAME}`,
  command substitutions and backticks remain literal text; nothing is expanded
  or executed.

`NAME_FILE` retains the existing bounded single-value-file behavior. If `NAME`
and `NAME_FILE` are both present after per-variable precedence, loading fails,
including when one comes from the process and the other from the env file.
An empty value still counts as present. A process `NAME_FILE` replaces the
env-file `NAME_FILE`; it does not suppress an env-file `NAME` conflict. These
conflicts remain errors even when a typed override targets the setting.

`Name` is a trusted, credential-free display name identifying the env file;
empty defaults to `.env`. It is separate from the filesystem path, following
TOML's source-label convention. A field loaded from line 3 records, for example,
`env-file:local.env:3:APP__HTTP__TIMEOUT`. A dotenv `NAME_FILE` binding records the
dotenv line and the `_FILE` key, never the mounted file's path or contents.
Process bindings retain `environment:NAME` / `environment-file:NAME`, and typed
overrides retain `override`. Secret markers come from the schema.

Parser and field-decoding errors include the source name and line when one is
available. Formatted errors and provenance omit values and filesystem paths;
retained I/O or decoder causes must not be logged. Invalid loads return no
partial settings or provenance. File paths are operator controlled; as with
TOML loading, this API does not promise cancellation of arbitrary filesystem I/O.

When adopting this option, delete application dotenv parsers, the merged lookup
closure and the provenance rewrite. Keep application defaults, declarations and
validation. Existing loaders can pass their selected process lookup unchanged:

```go
inputs := foundryconfig.Inputs[Settings]{
    EnvFile: foundryconfig.EnvFile{Path: options.EnvFile, Name: ".env"},
    Environment: environment,
    Overrides: options.Overrides,
}
// Keep the existing schema.Load / toml.LoadFile branch and Validate(settings).
// After successful validation:
return settings, report.Entries(), nil
```

## Public browser configuration

Mark browser-visible scalar or declared enum fields with `config:"name,public"`
and a `client:"env=..."` tag containing the complete frontend environment key.
The [compiled configuration declaration](../../tests/fixtures/consumer/configured/settings.go)
uses the same settings type for server configuration and public metadata:

<!-- compiled-source: tests/fixtures/consumer/configured/settings.go -->
```go
type Settings struct {
	Services      infrastructure.Settings
	AppName       string `config:"app_name,public" client:"env=VITE_APP_NAME,fallback=ConsumerApp"`
	DefaultLocale string `config:"default_locale,public" client:"env=VITE_DEFAULT_LOCALE,fallback=en,kind=locale"`
	FormatLocale  string `config:"format_locale,public" client:"env=VITE_FORMAT_LOCALE,kind=format_locale,optional"`
	TimeZone      string `config:"time_zone,public" client:"env=VITE_TIME_ZONE,kind=time_zone,optional"`
}
```

For this declaration, generation adds `SettingsPublicConfig() []config.PublicSetting`.
Pass its result to `manifest.Sources.PublicConfig` when building the contract
manifest, as the [client consumer](../../tests/fixtures/consumer/clientcontracts/contracts.go)
does. This exports setting metadata, not loaded server values. Secret fields,
secret groups and secret/password types cannot be public.

The client tag also accepts the bare `optional` flag, `fallback`, a positive `max`
length, and `kind`:
`string`, `boolean`, `number`, `locale`, `format_locale`, `currency`, `time_zone` or `enum`.
Catalog locale, formatting locale, currency and timezone kinds require string
fields; enum kind requires a declared enum. The TypeScript entry exports `PublicEnv`, `publicEnvSchema`,
`decodePublicEnv` and `findPublicEnvProblems` from this metadata.

For example, `client:"env=VITE_TIME_ZONE,kind=time_zone,optional"` declares an
optional time zone. For every kind, optional properties have TypeScript type
`T | undefined`; absent or blank values decode to `undefined`. Optional settings
prohibit nonempty fallbacks, including whitespace. Supplied values still undergo
the declared kind's validation; supplied `false` and numeric `0` remain values.

Public `number` values and fallbacks use finite JavaScript numbers: decimal
and scientific notation (such as `-12.5` or `1e2`), or unsigned integers with
`0x`, `0o` or `0b` prefixes. Go hexadecimal floats (`0x1p2`), numeric separators
(`1_000`), `NaN`, infinity and overflow are rejected during fallback generation
and client validation. Non-optional numeric fallbacks must be nonempty and have no
surrounding whitespace. Supplied environment values are trimmed; absent or blank
values use the declared fallback only for non-optional settings.

`kind=locale` requires exact membership in the message catalog and uses the
catalog's TypeScript `Locale` union. `kind=format_locale` is independent of that
catalog and emits `string`, or `string | undefined` with `optional`. Use it for
Intl date, number and money formatting; for example, `en-MY` is valid even if the
catalog only contains `en` and `ms`. Go validates non-optional fallbacks with
`golang.org/x/text/language`: they must be well-formed canonical BCP 47 tags.
Supplied browser values must be accepted by `Intl.getCanonicalLocales`. Malformed
tags such as `en_MY` are rejected; optional absent or blank values stay undefined.
