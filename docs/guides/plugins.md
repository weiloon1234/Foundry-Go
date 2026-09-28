# Plugins

Milestone 22 passed native full verification, independent consumer review,
PostgreSQL integration, compiler/editor checks, races and bounded fuzzing.

Plugins are ordinary Go packages linked explicitly into the application. A
constructor returns a `plugin.Module` or implements `plugin.Plugin`. Handle any
constructor error, then pass the plugin to `foundry.New().RegisterPlugin(...)`,
as the [independent consumer](../../tests/fixtures/consumer/pluginusage/bootstrap.go)
does. A plugin's `Register`
callback receives the same `foundation.Registrar` used by application modules;
no additional provider wrapper or package initialization is needed.

## Identity and lifecycle

`plugin.Manifest` declares a typed ID, full semantic version, framework version
requirement and typed dependencies. `plugin.FrameworkVersion` is the single
framework compatibility version, independent of the Go toolchain. Versions use
strict SemVer; requirements use [Masterminds semver](https://github.com/Masterminds/semver/tree/v3.5.0)
range rules. A prerelease needs an explicit prerelease comparator in the matching
range. Missing plugins, incompatible versions, cycles and duplicate IDs fail
before registration or boot. `plugin.Provider(id)` lets application providers
depend on a plugin through the existing provider graph.

`Register` and service constructors must remain pure and repeatable. Acquire
resources during `Boot` and immediately give each successful acquisition to
`Runtime.OnShutdown`. If boot subsequently fails, those acquired resources still
close. The plugin's `Shutdown` hook runs only after successful boot, before its
individual resource cleanups. Dependencies unwind in reverse order after owned
tasks exit. Foundation contains callback panics and `runtime.Goexit` without
formatting panic payloads.

`builder.Inspect(ctx)` validates declarations and records owners without running
service constructors, boot hooks, shutdown hooks or kernels. It does not consume
the builder. `app.Inspect()` returns an independent snapshot of the built graph.
Inspection includes contribution value/schema types and explicit replacement
provenance; it contains no service or configuration values.

## Direct contributions

Use the existing feature declarations with these registrar helpers:

| Feature | Plugin registration | Application assembly |
| --- | --- | --- |
| Typed service | `foundation.Provide` / `Factory` | Existing typed service graph |
| HTTP route or endpoint | `http.RegisterRoute` with its concrete descriptor and handler factory | `http.RegisterRouter`, resolved by the existing HTTP module |
| HTTP middleware | `http.RegisterMiddleware` | Applied outside each contributed route's own middleware |
| Job | `jobs.RegisterJob` with `Definition[P]` | Existing `jobs.Module` |
| Guard or policy | `auth.RegisterAuthorization` with the exact typed declaration | `auth.RegisterRegistry` |
| Event topic/listener | `events.RegisterTopic` / `RegisterListener` | Existing `events.Module` |
| Historical migration | `plugin.RegisterMigrations` | `plugin.RegisterMigrationRegistry`, then an explicit migration runner |
| Asset bundle | `assets.Register` | Explicit publication, never boot |
| Typed scaffold | `scaffold.Register` | Explicit rendering/publication, never boot |

Feature collections are scoped to their selected service. Missing assembly fails
Build. Job and event handler constructors can resolve their own dispatcher/bus;
binding happens separately before boot. HTTP route constructors resolve domain
services, not the router currently being assembled. Framework code can reuse
`foundation.Collection`, `ContributeAs` and `Contributions` to keep a concrete
schema type through an erased feature declaration, without a second registry.

Duplicates fail with contribution owners. Use
`builder.OverrideContributions("application", register)` for an intentional
application replacement. Every registration in that callback must replace an
existing contribution of the same type and schema; a missing target, schema
change or second replacement fails. Replacements keep their original position.
The migration, asset and scaffold registration helpers require a plugin
registrar and reject application override registrars. Whole-provider `Replace` remains separate and
cannot replace a plugin with an ordinary provider.

Route factories retain the declared transport's concrete Go path/query/body/
response types, method, path and access level through `RouteRegistration`.
Returning another transport under the same route ID fails application build.
Use the underlying transport descriptor for authentication/model-binding adapters;
they preserve its wire types. A factory may add URL signing, but a signed
declaration cannot return an unsigned registration. Runtime codec and validation
configuration still comes from the descriptor used to construct the handler.

## Configuration and migrations

`plugin.LoadConfig(manifest, schema, defaults, inputs)` calls the ordinary typed
configuration loader: owned defaults, ordered files, environment, then typed
application overrides. Keys must be descendants of `plugins.<plugin ID>`.
An ID containing characters outside the configuration grammar needs an explicit
`Manifest.ConfigNamespace`, such as `plugins.reports_pro` for `reports-pro`.
Namespaces cannot overlap between installed plugins. IDs are never sanitized.
Unknown file/override keys fail, and the returned report contains provenance and
secret flags without values. Construct configuration before registering the
plugin, and return loader errors from its constructor.

Plugin migrations use `plugin.MigrationOrigin(id)` across releases. Each
`migrate.Definition.Version` keeps its introducing semantic release, which cannot
be newer than the captured plugin version. An omitted origin is filled; a foreign
origin fails. SQL and dependency slices are copied. The existing migration
registry owns dependency ordering, checksums and historical validation. Build,
inspection and boot never apply migrations automatically.

## Assets and scaffolds

`plugin/assets.New(manifest, bundleID, files...)` snapshots `File{Path, Data}`
values. `plugin/scaffold.New[T](manifest, scaffoldID, renderer)` retains a typed,
context-aware renderer that returns the same file values. A scaffold renderer
uses ordinary Go functions, can validate its concrete input, and must finish
before returning. `Render` validates all returned paths and copies bytes before
any filesystem operation. `assets.Bundles` and `scaffold.Declarations` list
registered distributions without invoking renderers or boot hooks.

Publication selects one existing output root per distribution. The root's
ownership manifest records plugin, bundle/scaffold identity, release and file
digests. Other owners, release downgrades, edited owned files and unowned targets
are refused. Obsolete unchanged owned files can be removed during an update;
unrelated user files remain. A scaffold becomes editable consumer code, and a
later publication refuses to overwrite those edits.

The selected root is canonicalized. Paths within it are relative and portable:
no traversal, hidden files, symlinks, device names, case aliases or file/directory collisions. Bounds are 1,024 files, 8 MiB
per file, 32 MiB total, 256 parent directories and 16 path components. Nil bytes
create an empty file. Nested parents are created after staging; empty directories
can remain after failure or recovery because cleanup never recursively removes
directories that might contain user data.

`Publish(..., check=true)` is read-only and refuses pending recovery. Publication
uses the existing guarded planner, ownership checks, rollback and recovery with
a version 6 journal. An immutable ownership proof permits recovery after a second
interruption during rollback. Older journals do not gain permission to replace
ordinary asset/scaffold paths. Recover through `assets.Recover(ctx, root)` or
`foundry generate --recover --dir <root>`.

## Testing

`testkit.Plugins(t, plugins...)` composes the production Build/Start lifecycle
and test-owned cleanup. Use `testkit.Start` with a full builder when testing
application dependencies, custom runtime options or contribution overrides.
The independent [base](../../tests/fixtures/plugin_base/README.md),
[dependent](../../tests/fixtures/plugin_dep/README.md) and
[application](../../tests/fixtures/consumer/README.md) modules passed ordinary and
race checks with `GOWORK=off`. Acceptance covers real PostgreSQL history, actual
worker/event/route use, owned publication, repeated recovery and injected failures.
The full gate passed all 903 compiler-rejection cases and 357 editor scenarios,
including ten compiler cases and seven editor scenarios added for plugins.
Fifteen-second fuzz runs passed 434,543 path inputs and 1,412,117 manifest inputs.
Run `make verify` with the documented existing test services and language tools;
`make fixture-check` and `make race` include both independent plugin modules.
