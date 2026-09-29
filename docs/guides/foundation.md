# Application foundation

Milestone 24 delivered [shared observations](observability.md) through
`WithObservability`, `App.Observability`, `Runtime.Observability` and the synchronized
`Runtime.State` view. The application owns exporter startup and final draining;
the complete milestone verification batch passed. See [production acceptance](../production-acceptance.md).

Foundry assembles providers, constructs dependencies, owns application lifetime, and cleans up resources. The [independent consumer test](../../tests/fixtures/consumer/lifecycle_test.go) is the executable reference: it uses two fake providers and a cancellable kernel without a database or network server.

## Bootstrap and constructors

Use `foundry.New(options...).Register(providers...).Build(ctx)` in one application bootstrap function. `Build` validates provider dependencies, freezes registrations, and eagerly invokes typed constructors. It does not open resources or start a kernel. Duplicate provider, service, and kernel registrations fail. `Builder.Replace` deliberately replaces an existing provider. A builder is single-use; call the bootstrap function again for a second application or test.

Feature packages supply providers. A custom provider implements `foundation.Provider` and optionally `Dependent`/`Booter`; `foundation.Module` offers callbacks for the same contracts. Registration is deterministic; dependencies boot first. Missing dependencies and cycles fail before boot.

Declare each `foundation.Key[T]` once. `Provide(r, key, value)` binds a typed value; `Factory(r, key, constructor)` binds an explicit constructor that may call `Resolve(resolver, otherKey)`. Constructors run once, after all providers register. The returned type is inferred from the key. Pass the resulting concrete dependency to ordinary domain constructors. Missing services and constructor cycles fail `Build`.

Constructors and registration callbacks must be synchronous, pure assembly: no resource acquisition or background goroutines. Constructor calls are isolated and awaited; a panic or `runtime.Goexit` reports `fault.Panicked` before boot and expires the constructor's resolver. The resolver is not a request-time service locator. Runtime objects are application-owned and responsible for their own concurrency. Immutable registry bindings do not make arbitrary service objects thread-safe.

`ResolveAll[Contribution](resolver)` assembles every service registered with the exact declared `Contribution` type. Results follow provider dependency order and then registration order within each provider. Each constructor still runs once, including when an individual `Resolve` already constructed a contribution. Named and interface service types match their declared keys exactly. No matching declarations produce an empty typed slice. Constructor failures, missing dependencies and cycles fail assembly without publishing a partial collection.

The returned slice is a new slice; its service values retain ordinary application ownership. Resolve the contributions while constructing their feature, then inject that concrete feature into domain services. The [independent consumer](../../tests/fixtures/consumer/contributions_test.go) combines two providers' typed contributions using the same service graph and constructor dependencies. This constructor facility supports the next observer-registration integration; it does not register model observers by itself.

## Resource ownership and shutdown

Acquire external resources in `Boot(ctx, runtime)` and immediately register each successful acquisition with `runtime.OnShutdown(resourceID, close)`. This includes acquisitions made before a later boot failure. If registering cleanup fails, the acquiring code retains responsibility for closing the resource. Cleanups receive a separate shutdown context because the application lifetime is already canceled; its deadline is the remainder of the shutdown budget described below.

`runtime.Go(taskID, run)` starts a managed critical task. A non-cancellation error, panic, or `runtime.Goexit` cancels the application and remains in its final error. Cooperative cancellation alone is normal. Joined errors retain real failures even if another branch is cancellation. Tasks must observe their supplied context; durable work belongs in the [jobs subsystem](jobs.md).

`app.Start(ctx)` boots once; the first caller's context owns application lifetime, and its cancellation requests shutdown. Concurrent callers wait for that startup using their own deadlines. `WithStartupTimeout(d)` optionally bounds provider boot: expiry cancels the lifetime and `Start` reports `fault.Timeout` (logged as `application startup failed`) even when a provider returns only `ctx.Err()`, so a CLI reports a failure rather than an interrupt, while boot callbacks remain owned until they return. `app.Run(ctx, kind)` starts the same lifecycle, executes one registered kernel, and requests shutdown when the kernel completes. Available kinds are HTTP, CLI, worker, scheduler, and WebSocket; their concrete modules are delivered and must be registered by the application. `app.RunKernels(ctx, kinds...)` runs several distinct service kernels (for example HTTP and a worker) under one provider lifetime; the first to finish, a caller cancellation or a critical task failure stops all of them. The CLI kernel is one-shot and runs alone.

Cancelling the `Run` context (for example on SIGINT/SIGTERM through `signal.NotifyContext`) is a graceful stop. A service kernel that drains cooperatively and a shutdown that completes cleanly make `Run` return nil, so `cli.Report` exits 0; the executable needs no second `Shutdown` call. An interrupted CLI command keeps its cancellation error (exit 130). Each failure is reported exactly once: kernel errors come from the kernel result, boot errors from `Start`, and task/cleanup errors from shutdown.

Shutdown cancels work, waits for boot and managed tasks, then closes resources once in reverse acquisition order. Cleanup errors are combined with task errors and remain discoverable through `errors.Is`/`errors.As`; `Shutdown` also returns a boot failure. Panic payloads are not put into framework errors.

`WithShutdownTimeout` (default `DefaultShutdownTimeout`, 25 seconds) is one budget that starts when shutdown begins and covers the stop delay, kernel drain and every cleanup; cleanups share its remaining deadline. `Run` waits for this budget and reports `fault.Timeout` ("application shutdown is still pending") only when work outlives it. Keep kernel drain periods, such as the HTTP `ShutdownTimeout` grace, below it; configured assembly rejects budgets that leave no room for cleanup. `WithStopDelay(d)` adds a lame-duck period to a requested shutdown of a running application: `State` reports `Stopping` (readiness fails) while kernels keep serving, then admission closes and kernels drain. Kernel completion or failure stops immediately.

`app.Shutdown(ctx)` bounds the caller's wait. If a callback ignores cancellation, the application stays `Stopping`; it does not close dependencies underneath that callback or falsely report `Stopped`. Cleanup coordination continues after a caller times out. Inspect `PendingTasks`, wait on `Done`, or call `Shutdown` again. An executable may impose process termination; the framework never terminates its host.

The application logger records lifecycle events with safe structured fields: `application starting`, `application ready` (boot duration), `application startup failed`, `application shutdown started` (reason, stop delay, budget), `application cleanup failed`/`application cleanup timed out` (provider-qualified resource name and redacted `diagnostic`), and `application stopped` or `application stopped with failures`, which is written before `Done` closes so a process exiting when `Run` or `Shutdown` returns keeps it. Error text is never formatted.

Every application has a maintenance gate. `WithMaintenance(gate)` supplies one (for example a gate shared with a fleet-wide store); otherwise the recorder's gate or a fresh gate is used. `App.Maintenance()`/`Runtime.Maintenance()` return it and every runtime context carries it for `maintenance.FromContext`. Shutdown drains it after the stop delay.

## Typed configuration

[Generated configuration](generated-configuration.md) derives typed keys and the
same schema below from ordinary Go settings declarations. Its continuation
verification status is recorded in the master.

Each feature owns its settings struct, default values, keys, and validation. `config.New(fields...)` validates the schema. Keys carry both the settings owner and field value type. Built-in declarations cover string, integer, boolean, duration, and secret fields; `NewKey` accepts a typed decoder. `config.JSON` declares structured values such as slices, string-keyed maps, or structs, using a JSON text representation shared by file and environment sources. It rejects unknown struct fields and trailing values; custom JSON codecs retain their own semantics.

`schema.Load(defaults, config.Inputs[Settings]{...})` applies decoded file layers in order, then environment, then typed overrides. The [consumer fixture](../../tests/fixtures/consumer/lifecycle_test.go) uses `appName.Set("Foundry")`; incompatible values/owners are compiler errors. Environment names map `http.timeout` to `PREFIX__HTTP__TIMEOUT`, with no prefix separator when the prefix is empty. `Environment: os.LookupEnv` opts into the process environment; tests supply a lookup function.

Unknown file keys, invalid values, duplicate schema names, and ambiguous environment-name mappings fail. A field cannot also own a namespace: declaring both `http` and `http.port` fails. Invalid loads return zero settings and no partial report. `Report.Entries()` contains names, source labels, and secret flags, never values. Decoder/validation causes remain unwrap-accessible while the normal error message omits their contents.

Loads copy mutable default/override data so applications do not share reference-containing defaults. Supported configuration values are structs, scalars, pointers, slices/arrays, and maps with string keys; cycles, excessive nesting, and opaque mutable fields fail. Callbacks must be safe for concurrent use if the schema is shared. Returned settings are caller-owned values, not a global mutable configuration registry. Declare secrets as `secret.String` and call `Reveal` only at a credential boundary.

`config.Values` is the raw text-value boundary for file adapters. Import `config/toml` when TOML is needed. `toml.Decode(reader, schema, toml.Options{Name: "app.toml", MaxBytes: 4096})` returns a file layer; pass it to the same schema's `Load`. The [consumer configuration test](../../tests/fixtures/consumer/configuration_test.go) demonstrates typed declarations, a TOML layer, and a typed duration override. The caller opens/closes the reader and controls its deadlines; no file is loaded implicitly.

TOML decoding defaults to a 1 MiB input bound and reads at most one extra byte to detect overflow. Tables/dotted keys resolve only declared schema paths. Unknown settings, unknown empty tables, ambiguous quoted path segments, non-finite numbers, and nesting beyond 64 levels fail without returning a partial layer. At a declared collection key, nested map keys are data and retain literal dots. Arrays and owned tables become deterministic JSON; integers retain exact int64 values. Date/time values become ISO text and local values never acquire an implicit timezone. Scalar text is decoded by the key's normal parser, just as environment text is; file parsing alone does not validate settings.

Source names are trusted diagnostic labels, never credential-bearing URLs. Formatted errors omit document values, but unwrapped parser/I/O causes can contain input and must not be logged. TOML parsing is a separate import: environment and explicit overrides require no parser dependency.

## Logging, clocks, and tests

`WithLogger` injects an application logger; `logging.JSON` supplies a standard `slog` JSON handler. It redacts typed secrets and known credential keys/groups. Messages and arbitrary nested `slog.Any` objects still require application-owned safe logging contracts. No process-global logger is replaced.

`WithClock` injects application time exposed through `Runtime.Clock()`. `testkit.NewClock` supports concurrent `Now`, `Set`, and `Advance`. It changes application time, not Go timers or context deadlines; freezing it cannot freeze shutdown.

`testkit.Start(t, builder)` builds and boots the production path and registers cleanup before startup. It reports startup/shutdown failures through the test. The external fixture exercises this helper; `make fixture-check` also proves that incorrect service values, configuration owners/values, and model ID owners fail compilation.
