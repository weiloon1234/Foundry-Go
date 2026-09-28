# 02 — Foundation and application lifecycle

## Purpose and prerequisites

Prerequisite: [01](01-repository-and-toolchain.md). Establish the lifecycle all five kernels and every feature share.

Rust references: `src/foundation/app.rs`, `provider.rs`, `container.rs`, `background_tasks.rs`, `shutdown_drain.rs`; `src/kernel`; `src/config/mod.rs`; `src/logging`; `tests/acceptance.rs`, `logging_initialization_acceptance.rs`, `testing_layer_acceptance.rs`.

## Boundaries and public contracts

- `foundation` owns kernel/provider contracts and lifecycle coordination. It must not import concrete database, HTTP, auth, or storage packages.
- Root `foundry` assembles configured feature modules. Feature packages import foundational contracts, never the root assembly package.
- `config` owns loading and source attribution; each feature owns its typed configuration struct, validation, and defaults.
- `logging` adapts `slog` without taking ownership of a process-global logger. Embedding applications can supply their logger.
- Focused foundational types cover semantic IDs, model-specific UUID identities, nullable/presence values, clocks, dates/times, and error classification. Use `time.Time` internally where appropriate; preserve explicit date-only and local-time semantics at boundaries.
- `testkit` uses the same lifecycle path as production and supplies a controllable clock and cleanup helper.

Delivered kernel contract:

```go
type Kernel interface {
    Run(context.Context) error
}
```

The application builds and injects services before a kernel starts. Request-scoped dependencies are not stored on singleton services. Keep constructor injection as the normal application pattern; any registry type erasure remains internal, with typed registration and retrieval boundaries.

## Lifecycle and configuration

1. Load defaults, optional configuration files, environment overrides, then explicit programmatic overrides, in that order. Fail invalid known fields and report their source without values for secrets.
2. Validate provider/module identifiers and dependencies before starting resources. Duplicate registrations fail; explicit replacements are declared deliberately, not inferred from order.
3. Register services without starting background work; resolve dependencies and freeze registries.
4. Boot in dependency order. Track each successfully started resource.
5. Run the selected HTTP, CLI, worker, scheduler, or WebSocket kernel.
6. On cancellation or failure, stop accepting work, drain within configured deadlines, and close started resources in reverse order. Return combined shutdown errors without hiding the original failure.

Provide TOML file loading as a separately approved adapter to the typed config layer; environment and programmatic configuration must work without it. No reflection-driven application autowiring, hidden `init()` registration, or shared mutable builder defaults.

Delivered `config/toml` uses the approved [go-toml/v2 v2.4.3](https://github.com/pelletier/go-toml/releases/tag/v2.4.3), pinned in root `go.mod`. It bounds input bytes and normalization depth, resolves namespaces against `Schema.Names`, preserves source attribution, and rejects unsupported/unknown values without partial layers. Typed scalar/collection decoding remains in the same schema's `Load`. `config.JSON` provides structured text decoding across all sources. Tests cover nested tables, dotted keys, exact integers, arrays of tables, dates/times, malformed documents, size limits, parser/I/O error redaction, and layer precedence. The [foundation guide](../docs/guides/foundation.md) records the concrete consumer contract.

## Concrete core API and ownership

The [foundation guide](../docs/guides/foundation.md) links the compiling external consumer. `foundry.New(options...).Register(providers...).Build(ctx)` constructs the application without booting resources. `foundation.Module` is a convenience implementation of the small `Provider`, `Dependent`, and `Booter` contracts. `foundation.NewKey[T]`, `Provide`, `Factory`, and `Resolve` preserve service types; resolution belongs in bootstrap/constructors, followed by ordinary concrete constructor injection.

`App.Start` uses its first caller's context for application lifetime. `App.Run(ctx, kind)` selects one kernel and shuts down on completion or failure. `Runtime.Go` tracks critical cancellable work. `Runtime.OnShutdown` records each acquired resource immediately, including during a partially failed boot. Typed provider/task/resource identifiers are scoped structurally, so delimiters cannot create registration collisions.

A shutdown deadline bounds the caller's wait. Go cannot kill a goroutine: the application remains `Stopping` while uncooperative boot, work, or cleanup retains ownership. It drains managed tasks before closing dependencies, and closes resources in reverse acquisition order. Each cleanup receives a fresh deadline; panic/Goexit is isolated and reported. `Done` closes only when actual cleanup finishes. A hard process-termination policy belongs to the executable host, never a library `os.Exit` call.

The [typed values guide](../docs/guides/typed-values.md) defines delivered `model.ID[M]`, `value.Optional[T]`, `value.Nullable[T]`, and temporal boundaries. Feature-specific semantic IDs remain in their feature packages. Foundational model IDs introduce no database dependency.

## Implementation slices

1. Lifecycle contracts, typed errors, programmatic configuration, and a fake kernel/provider acceptance fixture.
2. Dependency validation, partial-boot cleanup, cancellation, deadline-aware shutdown, and owned goroutine tracking.
3. Typed environment decoding, secret handling, logging injection, clock and identity primitives.
4. Extension registration contracts used later by plugins; do not implement the full plugin manifest/assets system here.
5. Consumer bootstrap example and `testkit` integration. Keep future kernel implementations behind their own milestones.

## Failure behavior and tests

Test provider cycles, duplicate IDs, missing services, invalid configuration, zero providers, partial boot failure, cancellation before and during startup, repeated shutdown, drain timeout, and concurrent applications without state leakage. Run race tests against lifecycle and registration code. Verify errors retain `errors.Is`/`errors.As` behavior and logs redact secrets.

## Completion

Apply the [common gate](README.md#common-completion-gate). The independent fixture must assemble and stop a real application lifecycle with two fake providers and a cancellable kernel. Document ownership and startup/shutdown order before database work starts.
