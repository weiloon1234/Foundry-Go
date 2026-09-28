# 22 — Plugin ecosystem

## Purpose and prerequisites

Prerequisite: foundation's extension lifecycle and the feature registration interfaces already delivered. Make framework capabilities available to third-party Go packages with the same thin registration experience.

Rust references: `src/plugin`, `src/foundation/provider.rs`, `tests/plugin_acceptance.rs`, `plugin_fixture_acceptance.rs`, `tests/fixtures/plugin_*`, `docs/guides/plugins.md`, `blueprints/19-plugin-system-v2.md`.

## Public contract and ownership

Plugins are ordinary Go packages linked into the application through explicit registration. Do not use Go's platform-specific dynamic `plugin` loader or hidden package initialization for discovery.

Planned application contribution:

```go
builder.RegisterPlugin(reports.New(config))
```

A plugin supplies typed identity, version/compatibility manifest, dependency declarations and register/boot/shutdown behavior. Its registrar exposes the same feature contracts available to applications; it must not require a redundant provider wrapper to register jobs, routes, guards, events or middleware.

## Implementation slices

1. Manifest validation, dependency ordering, missing/version-incompatible dependency and cycle errors.
2. Typed feature contribution registry using foundation lifecycle and duplicate detection.
3. Namespaced configuration defaults, explicit application overrides and contribution inspection.
4. Versioned migrations, assets and scaffold distribution with ownership metadata and overwrite protection.
5. Independent plugin, dependency-plugin and consumer fixtures plus a public plugin test harness.

## Failure behavior

Duplicate contributions fail with both owners identified. Application replacement of a contribution requires an explicit override declaration. Plugin defaults precede application config according to the foundation loader; plugin-specific configuration lives in the plugin namespace.

Partial boot failure closes only successfully started resources in reverse dependency order. Asset/scaffold destinations must remain within the selected output root; path traversal and unintended overwrites fail before writing. Listing/inspection does not boot unnecessary runtime services.

## Acceptance

Test dependency cycles/missing versions, duplicate route/job/service identifiers, explicit overrides, config isolation, direct feature registration, partial-boot cleanup, shutdown order, asset ownership/path confinement and versioned migrations. Build independent fixture modules with public imports and without workspace-only resolution. Apply the [common gate](README.md#common-completion-gate).

## Delivered implementation and acceptance

The [plugin guide](../docs/guides/plugins.md) describes the current source contract:
typed manifests and semantic compatibility, the existing foundation lifecycle,
scoped typed contributions and explicit application overrides, pure declaration
inspection, namespaced configuration, historical migrations and owned asset/scaffold
publication through the shared publisher and a version 6 recovery journal.

Independent [base](../tests/fixtures/plugin_base/README.md) and
[dependent](../tests/fixtures/plugin_dep/README.md) plugin modules are consumed by
the existing application fixture with `GOWORK=off`. Test sources cover direct
runtime use, PostgreSQL history, compiler rejection, editor discovery and guarded
publication/recovery. The complete source phase preceded all compilation/tests.
Native `make verify`, independent ordinary/race modules, real PostgreSQL history,
worker/event/route execution, full publisher races and bounded path/manifest fuzzing
passed. All 903 compiler-rejection cases and 357 editor scenarios passed, including
ten and seven plugin additions respectively. Consumer experience was reviewed;
milestones 23–24 and the final framework-wide audit remain open.
