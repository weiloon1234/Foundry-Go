# C01 — Typed configuration and generation

## Consumer contract

Add `//foundry:config` to an exported, non-generic Go settings struct. Reuse
`foundry generate`, its graph discovery, type checking, manifests, stale checks,
atomic publication and crash recovery. No parallel generator command or manifest.

Configuration fields use `config:"name"`; names default to the existing generator's
snake-case convention. Concrete nested structs form dotted groups. `config:"-"`
explicitly excludes fields; embedded/private fields otherwise fail. Optional tag
options are `secret` and `json`: secret marks diagnostics provenance, while JSON
selects the existing structured-value codec rather than group flattening.

Generate `SettingsConfigKeys()` with concrete `config.Key[Settings, FieldType]`
members, including nested key sets, and `SettingsConfigSchema()` returning the
existing `*config.Schema[Settings]` plus error. Key setters preserve both owner
and value type. Import aliases and generated symbol collisions use the existing
generator machinery. The full overlay must compile before any output is published.

Example declaration (proposed until verified):

```go
//foundry:config
type Settings struct {
    HTTP struct {
        Listen string        `config:"listen"`
        Timeout time.Duration `config:"timeout"`
    } `config:"http"`
    SigningKey secret.String `config:"signing_key"`
}
```

Defaults remain a normal Go value/function. Validation remains a normal typed
function supplied through `config.Inputs[Settings].Validate`; the generator does
not encode executable defaults or business validation into strings/tags. Both
are used by the same loader for files, environment and typed overrides.

## Codec and validation rules

- Preserve named scalar types, integer widths, booleans, finite floats,
  `time.Duration`, `secret.String`, generated enum membership and supported text
  unmarshallers. Conversion errors return no partially loaded configuration.
- Reuse `config.JSON` for explicitly structured fields, slices and string-keyed
  maps. Its existing JSON representation and custom-codec semantics remain
  authoritative. Do not silently advertise string duration decoding inside an
  opaque JSON object when its Go type lacks that codec.
- Reject function/channel/interface/pointer configuration leaves without a
  supported explicit codec; reject recursive groups, duplicate names, environment
  collisions, namespace/leaf collisions and invalid tags before publication.
- Secret fields are automatically sensitive; inherited secret groups must keep
  every leaf sensitive. Names/provenance may be inspected, values may not be logged.
- Add reusable scalar/text declaration helpers only where the existing config
  package lacks them. Generation calls those helpers instead of duplicating
  parsers in each output file.
- Add a bounded TOML file-loading convenience on top of `toml.Decode` and
  `Schema.Load`. It owns file close, returns safe source labels, exposes injected
  environment lookup and does not implicitly load `.env` or mutate the process.
- Runtime file values remain runtime validated. Generated types prove Go API
  compatibility, not that an arbitrary external name/port/credential is valid.

## Verification

Implement the complete source/test/doc batch first. Test deterministic fresh and
repeated generation, read-only stale checks, fresh consumer references to generated
symbols, graph composition, nested fields, alias/import collisions, malformed and
unsupported declarations, secret handling, exact scalar bounds, file limits,
unknown keys, precedence, mutable-default isolation and validation failures.

Independent consumers exercise generated keys with TOML/environment/overrides.
Compiler-negative cases reject wrong owner and value types. Real gopls completes
nested keys and their typed `Set` values. Run the native full gate and relevant
race/generation/editor checks before marking C01 complete.
