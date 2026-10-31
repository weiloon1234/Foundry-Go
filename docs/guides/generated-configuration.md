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
defaults, file layers, environment, typed overrides. `APP__HTTP__TIMEOUT` targets
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
}
```

For this declaration, generation adds `SettingsPublicConfig() []config.PublicSetting`.
Pass its result to `manifest.Sources.PublicConfig` when building the contract
manifest, as the [client consumer](../../tests/fixtures/consumer/clientcontracts/contracts.go)
does. This exports setting metadata, not loaded server values. Secret fields,
secret groups and secret/password types cannot be public.

The client tag also accepts `fallback`, a positive `max` length, and `kind`:
`string`, `boolean`, `number`, `locale`, `currency`, `time_zone` or `enum`.
Locale, currency and timezone kinds require string fields; enum kind requires
a declared enum. The TypeScript entry exports `PublicEnv`, `publicEnvSchema`,
`decodePublicEnv` and `findPublicEnvProblems` from this metadata.
