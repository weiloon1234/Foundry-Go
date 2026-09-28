# Developer tooling and testing

Milestone 23 passed native verification, independent consumer review, relevant
races and bounded fuzzing. This guide explains the public APIs and their
executable consumer recipes.

## Commands and bootstrap

Declare an argument struct, bind it with `cli.Flags`, and pass the decoder to
`cli.Define`. `Command[A].Declare` accepts a constructor returning `cli.Handler[A]`;
the compiler retains the same argument type through decoding and execution.
`cli.New` freezes explicit declarations and rejects duplicate names. `Parse`
validates arguments and prints help before application startup. The selected
handler is constructed only when its invocation runs.

The [command consumer](../../tests/fixtures/consumer/tooling/commands.go) is the
complete recipe: typed flags, validation, injected services, explicit streams,
shared `cli.Module` bootstrap and a metadata command. Domain commands use the
normal CLI kernel and provider shutdown. An invocation runs once, including on
failure; parse another invocation for an intentional retry. Cancellation before
execution does not acquire that execution claim. A custom decoder can implement
positional arguments or nested subcommands without a second command registry.

`cli.Report` writes an error and returns a status for the application's main:
0 for success/clean help, 1 for execution/reporting failure, 2 for invalid usage,
124 for deadline expiry and 130 for cancellation. The framework library never
calls `os.Exit`. The development binary owns signal cancellation and its exit.
Status inspection bounds cyclic or excessively large/deep error graphs and
reports them as execution failures. Custom error methods must return and make
shallow `Is`/`As` comparisons; panic and Goexit stay inside owned isolation.
Handlers and constructors must honor context cancellation; the framework contains
panic/Goexit, but cannot forcibly stop arbitrary application goroutines.

## Individual artifact scaffolds

Prefer the [project-pinned CLI](team-adoption.md) through `go tool foundry`, which
shares the application's framework requirement. An installed `foundry` must match
that dependency. In this checkout the same command is `go run ./cmd/foundry`.
Select an existing Go package directory inside the consumer module. For a new
package, first add a `doc.go` containing its package declaration, as shown in
[team adoption](team-adoption.md). An empty or missing directory is not a Go package.

Then create individual declarations:

```sh
foundry make model Widget --table widgets --dir ./models
foundry make dto WidgetResponse --dir ./transport
foundry make job DeliverWidget --id widget.deliver --queue deliveries --dir ./jobs
foundry make command InspectWidgets --id widgets.inspect --dir ./commands
foundry generate --recursive --dir .
foundry generate --recursive --check --dir .
```

These commands create one consumer-owned artifact, never an application project.
Existing migration/seeder scaffolds use the same path checks and publisher.
Models require an explicit table. DTO identity comes from its Go type. Jobs and
commands require semantic IDs. Add fields/behavior to the handwritten artifact,
then run generation for typed model/DTO metadata. Job and command placeholders
return an explicit error until implemented and registered. Scaffolds never
silently overwrite existing files; recovery preserves create-only authority.

Fresh-package generation, installed dependency compatibility and collisions are
covered by the [scaffold tests](../../internal/generate/scaffold_developer_test.go).

## Doctor and declaration inspection

```sh
foundry doctor --dir . --go go --gopls gopls --require-gopls
```

Doctor reads the selected Go/module metadata and optionally checks gopls. It
compares actual module Go requirements, bounds each process and its output,
disables downloads, and reports actionable required-check failures. It does not
compile consumer packages, start services, load application configuration or
install tools. Omit `--require-gopls` when editor tooling is optional.

`inspection.Collect` accepts existing route, job, schedule, CLI and contract
registries, a builder and configuration reports. It reuses their metadata and
returns owned snapshots. Builder inspection invokes repeatable registration
callbacks; it does not construct services or boot providers. Registration must
therefore remain declarative. Configuration output contains names, sources and
secret markers, never setting values.

`inspection.Command` accepts a pure collector so it can include the final CLI
registry itself. The consumer runs this invocation before `Build`/`Run`, keeping
metadata inspection independent of service startup. Select a typed section
(`routes`, `jobs`, `schedules`, `plugins`, `commands`, `configuration`, `contracts`
or `all`) and compact JSON or readable indented text. The development binary
cannot import an arbitrary application's bootstrap: register this command in the
application that owns those declarations.

## Test-owned application and transports

`testkit.Start` uses the actual builder and lifecycle and registers cleanup before
startup. Partial boot failure and later `t.Fatal` still release registered
resources. Register application cleanup before clients so clients close first.
A [failing-child-test acceptance](../../testkit/cleanup_test.go) checks this boundary.

`testkit/http.New` serves the supplied production handler on an owned local
server. Its embedded production `httpclient.Client` uses bounded requests and no
retries. `JSON`, `DecodeJSON` and `AssertStatus` keep generated request/response
contracts explicit. Requests traverse the handler's real middleware and auth.
See the [HTTP recipe](../../tests/fixtures/consumer/tooling/tooling_test.go).

`testkit/websocket.Connect` owns a real connection and handshake. Send protocol
requests, receive bounded frames, assert their type and decode event payloads
with `DecodePayload` and the generated DTO contract. The
[realtime consumer](../../tests/fixtures/consumer/realtime/orders_test.go) exercises
this through a real kernel, native middleware, broadcast and replay.

## Typed factories and explicit fakes

The [factory recipe](../../tests/fixtures/consumer/tooling/database.go) returns
`Factory[models.WriteRecord, models.WriteRecordDraft]`. `Draft` builds without SQL;
`WithStates` appends ordered typed transformations. Derived states share a
monotonically increasing sequence; separate factories own independent sequences.
Failed builds consume their number. Shared callbacks must support concurrency.

`Create` and `CreateMany` use normal generated mutation preparation and model
lifecycle hooks. The bounded batch uses per-model `InsertEach`, with atomic
rollback and real after-commit timing. It never switches to a hook-free bulk
insert. The [PostgreSQL factory consumer](../../tests/fixtures/consumer/softqueries/factory_postgres_test.go)
checks defaults, UUIDs, mutators, timestamps, observer order and rollback.

The [local capability recipe](../../tests/fixtures/consumer/tooling/helpers_test.go)
uses explicit helpers with production contracts:

| Helper | Behavior |
| --- | --- |
| `testkit.NewClock` | Independent controllable application time; context deadlines still use real time. |
| `testkit/storage.Local` | Real local disk in a test-owned temporary directory, closed by cleanup. |
| `testkit/email.New` | Bounded memory driver used by the normal mailer; assertions omit private message content. |
| `testkit/jobs.New` | Independent memory backend and namespace, real declarations, typed capture/dispatch and deduplication. Workers run only when explicitly started. |
| `testkit/events.New[E]` | Bounded immutable listener snapshots, normal bus dispatch and caller-owned transaction/after-commit semantics. |
| `testkit/auth.Scope` / `Require` | Existing real verifier/guard/policy behavior; helpers do not bypass eligibility or MFA requirements. |

Use `testkit.Namespace` for an independent cache/coordination namespace. Existing
PostgreSQL helpers create isolated identities/records; they never authorize
resetting shared data. No helper replaces global state or resets a database.

## Query plans and acceptance workflow

[Typed PostgreSQL query plans](query-plans.md) reuse compiled SQL and bindings;
ordinary inspection and explicit execution analysis have separate methods.

Finish a milestone's implementation, tests, examples and docs before compilation
or testing. Then run the full milestone batch, collect failures, fix them as a
batch and repeat affected checks. Accept only after final-source `make verify`
and required integration/race/editor checks pass. The
[language tooling guide](agent-language-tooling.md#verification-batches-and-cache-freshness)
explains scenario batching and external-input cache freshness. Skipped external
checks and unexecuted tests are not acceptance evidence.

## Continuous verification and security

The repository workflow (`.github/workflows/verify.yml`) reuses Makefile gates
on native macOS with isolated runner-local PostgreSQL/Redis. It requires backend,
real gopls and TypeScript evidence. Dependency update configuration (`.github/dependabot.yml`)
keeps proposals reviewable; it does not automatically merge or publish them.

`make security-check` is independent of cold package measurements. Select existing
`FOUNDRY_TEST_GOVULNCHECK`, `FOUNDRY_TEST_GOPLS`, `FOUNDRY_TEST_NODE` and
`FOUNDRY_TEST_NPM` executables; the command installs nothing and reuses warm caches.
It scans the framework, release tool, all independent fixtures, the selected gopls
binary and the TypeScript lockfile. `SECURITY_OUTPUT` selects its evidence directory
(default `.cache/security-check`). Each report retains scanner/database metadata,
actual findings and failure dispositions. Known affected functions, npm findings,
missing/malformed results and tool failures fail the gate even when JSON-mode
scanner exit status is zero. Module/package-only findings are preserved for review.
See the [security policy](../../SECURITY.md) for private disclosure.
