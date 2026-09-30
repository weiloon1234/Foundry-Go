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
execution does not acquire that execution claim. `cli.Flags` rejects positional
arguments; `cli.FlagsWithArgs(configure, positional, validate)` binds the values
remaining after flags (and after `--`) into the same argument struct, and
`cli.ExactArgs(n, assign)` enforces a count. Positional errors are usage errors.
A custom decoder can still implement nested subcommands without a second registry.

Destructive commands can call `cli.Confirm(ctx, streams, prompt, force)`, which
accepts only `y`/`yes` from an interactive input and refuses a non-terminal file
input (pipes, `/dev/null`) with `cli.ErrNotConfirmed` unless `force` is set;
`cli.ConfirmInProduction` asks only when its `production` argument is true.
`cli.WriteTable(output, header, rows)` writes aligned columns and replaces control
characters inside cells, so data cannot forge rows or columns.

Commands are rejected while application maintenance is paused. Declare an
operational command with `Command.AllowDuringMaintenance()`, or run one invocation
with the leading `--during-maintenance` flag (`app --during-maintenance migrate`);
draining still rejects both. `application.MaintenanceCommands()` returns the `down`
and `up` declarations (see [production operations](production-operations.md)), and
`application.AboutCommand(name, settings)` prints the framework version, Go
toolchain, platform, namespace, timezone, enabled kernels and configured service
drivers (names and driver kinds only; hosts, users and credentials are omitted) as a
table or `--json`, without building the application.

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

The other kinds follow the same pattern:

| Command | Creates |
| --- | --- |
| `make endpoint CreateNote --id notes.create --method POST --path /notes` | `CreateNoteRequest`/`CreateNoteResponse` DTOs (no request DTO for `GET`), their generated codecs, then the typed route, `CreateNoteEndpoint()` and a `HandleCreateNote` stub. Paths are static; add a `//foundry:path` struct for parameters afterwards. |
| `make enum NoteState --cases draft,published` | A `//foundry:enum` string type with one constant per case. |
| `make event NotePublished --id notes.published` | A payload struct and its `events.Topic`. |
| `make listener IndexNote --id notes.index --event NotePublished` | An `events.Listener` for an existing payload type and its handler stub. |
| `make policy UpdateNote --id notes.update --subject Member --resource Note` | An `auth.Policy` that denies until implemented. |
| `make middleware Audit --id notes.audit` | A pass-through `http.Middleware`. |
| `make rule Slug --id notes.slug` | A `validation.Custom` string rule. |
| `make notification NoteShared --id notes.shared` | A `NoteSharedPayload` DTO, its codec and the `notifications.Definition`. |
| `make migration CreateNotes --id ... --origin app --version v1.0.0 --create notes` | A migration starting from a reviewed `CREATE TABLE` with a UUID key and timestamps. |

Endpoint and notification scaffolds check every target first, then create each
DTO, run generation for the package and create the component file; each step
publishes through the same guarded path, so a later failure leaves earlier,
valid files in place. Test files and factories remain handwritten: they are not
package declarations the scaffold type checker can verify.

These commands create one consumer-owned artifact, never an application project.
Existing migration/seeder scaffolds use the same path checks and publisher.
Generation never edits handwritten files unless `foundry generate --field-docs`
opts into [managed field notices](model-accessors.md#notices-beside-the-actual-field).
Models require an explicit table. DTO identity comes from its Go type. Jobs and
commands require semantic IDs. Add fields/behavior to the handwritten artifact,
then run generation for typed model/DTO metadata. Job and command placeholders
return an explicit error until implemented and registered. Scaffolds never
silently overwrite existing files; recovery preserves create-only authority.

Fresh-package generation, installed dependency compatibility and collisions are
covered by the [scaffold tests](../../internal/generate/scaffold_developer_test.go)
and the [component scaffold tests](../../internal/generate/scaffold_components_test.go).

## Doctor and declaration inspection

```sh
foundry doctor --dir . --go go --gopls gopls --require-gopls
```

Doctor reads the selected Go/module metadata and optionally checks gopls. It
compares actual module Go requirements, bounds each process and its output,
disables downloads, and reports actionable required-check failures. Its
`tool-version` check compares the framework release the running `foundry` was
built from with the version the module selects; a known mismatch fails, while
development builds and local `replace` directories are reported as unverifiable.
`foundry generate` rejects the same mismatch before analysis. It does not
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
(`routes`, `jobs`, `schedules`, `plugins`, `commands`, `configuration`, `contracts`, `extensions`
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
retries or redirect following. Each request is limited to `DefaultTimeout` (five
seconds); pass `WithTimeout` to `New`/`Connect` for slow handlers, and
`WithHeaders` for headers on every request. On a derived client each named
header replaces the inherited value rather than adding a second one. `client.With(t, options...)` derives
an independent client for the same server. `JSON`, `DecodeJSON` and
`AssertStatus` keep generated request/response contracts explicit. Requests
traverse the handler's real middleware and auth.
See the [HTTP recipe](../../tests/fixtures/consumer/tooling/tooling_test.go).

Response assertions report failures through the test without printing secrets:

| Helper | Checks |
| --- | --- |
| `AssertJSONPath(t, response, "/data/items/0", want)` | The value at an RFC 6901 pointer equals `want` encoded by Go's JSON encoder, ignoring key order. |
| `AssertValidationErrors(t, response, "email", "/items/0/name")` | A 422 `validation_failed` envelope with an issue at each wire path. |
| `AssertHeader`, `AssertCookie` | A header value; exactly one `Set-Cookie` for a name, returned for attribute checks. |
| `AssertRedirect`, `AssertNoContent` | A 3xx with an exact `Location`; a 204 with an empty body. |

`ActingAsToken(t, client, tokens, proof, options)` issues a real access token
through the application's configured `token.Tokens` binding and returns a client
sending it as a bearer credential, replacing any inherited `Authorization`.
`ActingAsSession` issues a real session and sends it in the declared credential
cookie, replacing an inherited cookie of that name and keeping other cookies in
one `Cookie` header. Re-authenticating a client therefore never sends two
credentials, which the framework rejects as ambiguous. Build `proof` with `auth.NewProof`
for the actor and assurance under test. Every request still verifies the
credential, provider eligibility, scopes, policies and, for cookies, the
application's origin/CSRF policy; the helpers grant nothing themselves.

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

Composition stays typed:

- `DraftWith(ctx, overrides...)` and `CreateWith(ctx, writer, overrides...)`
  apply per-call `State` overrides after the factory's own states without
  retaining them, for example `func(_ context.Context, d NoteDraft) (NoteDraft, error) { return d.SetTitle("Pinned"), nil }`.
- `AfterCreating(hooks...)` derives a factory whose hooks run in order after each
  created model, inside the transaction that inserted it (a savepoint within
  the caller's transaction); a hook can create related records or return a
  reloaded model. A failing or panicking hook rolls back its model and, for
  `CreateMany`, the whole batch, even with a pool writer. Hooks are bounded by
  `MaxHooks` and contained like other callbacks.
- `factory.CreateFor(ctx, writer, child, parent, assign)` creates a parent through
  its factory and one child that belongs to it; `factory.CreateHas(ctx, writer,
  owner, child, count, assign)` creates an owner and a bounded batch of children.
  `assign` copies the parent's key into the child's draft.

`factory.NewFaker(seed)` is a small deterministic generator of fictional values
(`Name`, `Email` at the reserved `example.test` domain and unique per faker,
`Word`, `Words`, `Sentence`, `IntBetween`, `Float`, `Bool`, `TimeBetween`, and
`factory.Pick`). The same seed reproduces a failing test's values; it uses only
the standard library.

`testkit/dbassert` asserts persisted state through the typed generated queries
applications already use: `AssertExists`, `AssertMissing`, `AssertCount` and
`AssertSoftDeleted` take a generated query (for example
`QueryNotes().Where(...)`) and the test's executor or transaction. Model scopes,
including soft-delete visibility, apply as in application code; the helpers only
read and never reset or seed data.

The [local capability recipe](../../tests/fixtures/consumer/tooling/helpers_test.go)
uses explicit helpers with production contracts:

| Helper | Behavior |
| --- | --- |
| `testkit.NewClock` | Independent controllable application time; context deadlines still use real time. |
| `testkit/storage.Local` | Real local disk in a test-owned temporary directory, closed by cleanup. |
| `testkit/email.New` | Bounded memory driver used by the normal mailer; `AssertSent`, `AssertNotSent` and `AssertSentCount` take typed message predicates; assertions omit private message content. |
| `testkit/jobs.New` | Independent memory backend and namespace, real declarations, typed capture/dispatch and deduplication. Workers run only when explicitly started. `AssertPushed`, `AssertNotPushed` and `AssertPushedCount` decode retained payloads of a typed definition and apply a typed predicate without printing payloads. |
| `testkit/events.New[E]` | Bounded immutable listener snapshots, normal bus dispatch and caller-owned transaction/after-commit semantics. |
| `testkit/events.NewFake` | Intercepts a production bus so listeners do not run; `AssertDispatched`, `AssertNotDispatched` and `AssertDispatchedCount` take typed topics and payload predicates. |
| `testkit/notifications.New[D]` | Recording `notifications.Custom` transport with a configurable outcome (`Respond`), so tests exercise the production manager and claim protocol; `AssertDelivered`, `AssertNotDelivered` and `AssertDeliveredCount` take typed output predicates. |
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

## Optional frontend adapter acceptance

`tools/typescript/package.json` declares development-only React, React DOM, Vue,
DOM test support and declaration packages alongside the pinned compiler. Install
these with the repository's normal dependency-approval policy before requiring
`make typescript-check`. The core generated SDK has no runtime npm dependencies.
The client gate compiles/runs ordinary TS/JS HTTP/WebSocket contracts and the
optional real React/Vue adapter checks. Missing peers fail required-mode checks.
External-input fingerprints include the selected compiler's dedicated
`node_modules` tree, including transitive adapter implementations and types.
