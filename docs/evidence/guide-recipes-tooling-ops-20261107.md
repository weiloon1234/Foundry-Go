# Milestone 3 tooling and operations recipe mapping

Implementation handoff for `lane/m3-tooling-ops`, 2026-11-07. This record covers
Starter's `artisan`, API portions of `testing`, `foundry-operations`, and API
portions of `portals`. It does not claim acceptance of this revision.

Sources were read from `/Users/weiloon/Projects/rust-go/Foundry-Go-Starter/.claude/skills/`.
Line references below refer to those files as read on this date, including their
`this-app` regions. The matching ooo artisan and portals skills were also read.
Their service-free inventory and manifest-derived browser-base guidance agree
with the Starter rules. Application repositories were read only.

## Artisan

| Starter skill lines | Framework recipe owner | Consumer source region or proof boundary |
| --- | --- | --- |
| `artisan:16–18` | [Pinned API lookup and artifact generation](../guides/developer-tooling-and-testing.md#recipe-scaffold-artifacts) | Shell commands; no Go compiler-proof claim |
| `artisan:19–21`, `73` | [Inspect a source symbol](../guides/agent-language-tooling.md#recipe-inspect-source) | Pinned CLI shell commands; existing gopls acceptance remains the proof owner |
| `artisan:22–24`, `65–67` | [Scaffold artifacts](../guides/developer-tooling-and-testing.md#recipe-scaffold-artifacts), [model scaffold](../guides/model-scaffolding.md#recipe-scaffold-model), [slot flags](../guides/model-scaffolding.md#recipe-scaffold-slots) | CLI shell examples; scaffold output remains owned by existing generator scaffold tests |
| `artisan:25–28`, `61` | [Register tables](../guides/model-declarations.md#recipe-register-tables), [add fields](../guides/model-scaffolding.md#recipe-add-fields), [add relations](../guides/model-scaffolding.md#recipe-add-relations) | `goway/migration/examples_test.go#application-after` and `productionprofile/queries.go#check-table-registry`; existing `goway/guideadapters/scaffold_test.go#model-scaffolding-name-input` and `#model-scaffolding-child` |
| `artisan:28–31`, `68–69`, `76` | [Generation and recovery](../guides/developer-tooling-and-testing.md#recipe-scaffold-artifacts) | Pinned CLI generation/check commands and generator ownership prose |
| `artisan:35–39` | [Typed commands and bootstrap](../guides/developer-tooling-and-testing.md#recipe-declare-command) | `tooling/commands.go#typed-command`, `#parse-before-bootstrap` |
| `artisan:40–42`, `71–72` | [Declaration inspection](../guides/developer-tooling-and-testing.md#recipe-inspect-declarations) | `bootstrap/redis_inventory_test.go#redis-inventory`; existing surrounding regression checks no provider startup or credential retrieval |
| `artisan:70` | [Development supervision](../guides/developer-tooling-and-testing.md#development-server-supervision-loop) | Existing reference section; app launcher and Vite process choices remain local, with no extracted Go API sequence in this skill |

The dotted catalog names, colon aliases, `withoutWeb`, local confirmation policy,
launcher, paths and catalog implementation at lines 43–74 stay application-owned.
The framework CLI example retains its own public registry and parse-before-boot
contract rather than copying the app's console abstraction. No unresolved
artisan extraction remains in this lane; shell examples do not claim compilation.

## Testing

| Starter skill lines | Framework recipe owner | Consumer source region or proof boundary |
| --- | --- | --- |
| `testing:43–46` | [Run test-owned kernels](../guides/isolated-http-tests.md#recipe-run-kernels) | `tooling/guide_recipes_test.go#run-http-test`, `#run-worker-test` |
| `testing:53`, `106–108` | [Register and check native tables](../guides/model-declarations.md#recipe-register-tables) | `goway/migration/examples_test.go#application-after` and `productionprofile/queries.go#check-table-registry`; registry completeness remains an explicit app assertion, not package discovery |
| `testing:54`, `71–78` | [Isolate an application](../guides/isolated-http-tests.md#recipe-isolate-application), [schema cleanup](../guides/isolated-http-tests.md#recipe-schema-cleanup) | `tooling/guide_recipes_test.go#isolated-application`; existing `testkit/postgres` cleanup implementation and retention regressions own pass/fail/panic behavior |
| `testing:79–81` | [Redis test configuration](../guides/isolated-http-tests.md#recipe-redis-test-config) | `tooling/guide_recipes_test.go#redis-test-settings`; conversion preserves parsed user/password and uses default connection limits |
| `testing:82–84` | [Missing attachment original](../guides/isolated-http-tests.md#recipe-missing-attachment) | `tooling/guide_recipes_test.go#missing-attachment-object`; concrete `attachments.File[models.User]` |
| `testing:62–65` | [Artifact generation](../guides/developer-tooling-and-testing.md#recipe-scaffold-artifacts) | Existing generator ownership and generation triggers; model/table/row edits remain handwritten |
| `testing:103–108` | [Configured startup](../guides/consumer-startup-acceptance.md#recipe-run-consumer) | `configuredprofile/cmd/profile/main.go#run-configured-http`; app-specific testsupport and bootstrap functions remain local |

Cadence, browser orchestration, execution switches, test selection and local
scripts at lines 13–39, 48–60, 66–67, 85–95 and 99–136 remain app workflow.
There is no unresolved testing API extraction in this lane. New helper functions
are compiler inputs, not newly executed integration evidence. The HTTP example
is a runnable test; the worker, isolation, Redis and attachment examples are
focused typed helper recipes for applications with those resources.

## Foundry operations

| Starter skill lines | Framework recipe owner | Consumer source region or proof boundary |
| --- | --- | --- |
| `foundry-operations:20–22` | [Declare readiness](../guides/readiness-and-maintenance.md#recipe-declare-readiness), [startup diagnosis](../guides/production-operations.md#recipe-startup-readiness) | `tooling/readiness.go#feature-readiness`; dependency errors remain outside public readiness responses |
| `foundry-operations:34–41`, `46–47`, `76–87` | [Pinned deployment invocation and role lifecycle](../guides/deployment.md#recipe-deploy) | Existing deployment tool contract and operator examples; no Go/TS fence or host execution claim |
| `foundry-operations:39–45` | [Rolling termination](../guides/production-operations.md#recipe-rolling-termination), [recovery evidence](../guides/production-operations.md#recipe-recover-operations) | `tooling/readiness.go#pause-admission` is included once by [maintenance admission](../guides/readiness-and-maintenance.md#recipe-maintenance-admission); deployment keeps detailed host/backup/reconciliation ownership |
| Deployment's existing application storage-probe adapter | [Probe release storage](../guides/deployment.md#recipe-storage-probe) | `tooling/readiness.go#storage-probe`; replaced app-specific `Console`, catalog and loader examples with the public helper boundary |

The following recipes already have other guide owners. Production operations
links to them instead of duplicating their API teaching. Their current proof is
outside this lane; this handoff makes no new compiler or runtime claim for them.

| Starter skill lines | Linked owner | Scope |
| --- | --- | --- |
| `foundry-operations:16–19` | [Logging](../guides/logging.md), [observability](../guides/observability.md) | Injected logger, correlation, safe fields and bounded labels |
| `foundry-operations:23–25` | [Production diagnostics](../guides/production-diagnostics.md) | Authorized rotating-file maintenance, opaque IDs, audit and ambiguous outcomes |
| `foundry-operations:27–30` | [Caching](../guides/caching.md), [distributed cache](../guides/distributed-cache.md), [leases](../guides/leases.md) | Typed views, invalidation, named stores and retained work ownership |
| `foundry-operations:31–33` | [Encryption](../guides/encryption.md), [audit](../guides/audit.md) | Purpose-bound stored secrets and typed field audit |
| `foundry-operations:66–71` | [Persisted telemetry](../guides/persisted-telemetry.md) | Configured telemetry, borrowed `Services.Telemetry()` and source-map owner; app console/actor policy and historical tables remain local |

No unresolved operations recipe remains within the lane-owned guides. Linked
owners must retain their own proof and extraction coverage. App developer access
policy, concrete disk choices, grants and deployment authorization remain local.

## Portals

| Starter skill lines | Framework recipe owner | Consumer source region |
| --- | --- | --- |
| `portals:20–23`, `49–51`, `62–66` | [Assets and client navigation](../guides/http-assets.md#recipe-serve-assets), [configured SPA](../guides/http-assets.md#recipe-declare-spa) | `httpassets/assets.go#asset-lifecycle`, `httpassets/runtime.go#configured-spa` |
| `portals:51–53`, `74–79` | [Public runtime DTO and descriptor](../guides/http-assets.md#recipe-spa-runtime) | `httpassets/runtime.go#portal-runtime-dto`, `#portal-runtime`; generated codec and ownership manifest retained |
| `portals:77–78` | [Development proxy adapter](../guides/http-assets.md#recipe-runtime-proxy) | `httpassets/runtime.go#runtime-proxy`; caller supplies its navigation predicate and reserved-path policy |

The `path`, `root`, `api_only` manifest schema, URL inventory, browser bases,
SSR ownership, strict-decoder reader implementation, Vite processes and builds
remain application-owned. The guide retains runtime HTML `no-store`, immutable
hashed-bundle policy, direct-navigation exclusions, CSP and callback ownership.
App-by-app migration instructions were replaced with neutral integration guidance.
There is no unresolved portals API extraction in this lane. No new TypeScript
example was added, so no TypeScript input list changed.

## Files and proof boundaries

All nine lane-owned guides now have navigation-only Agent entries and explicit
recipe anchors. Rendered Go fences in their recipe sections come from consumer
regions. Existing scaffold examples keep their single Go-way source owner.
No markers were added outside the lane's owned fixture packages.

Changed guides:

- `docs/guides/developer-tooling-and-testing.md`
- `docs/guides/model-scaffolding.md`
- `docs/guides/agent-language-tooling.md`
- `docs/guides/isolated-http-tests.md`
- `docs/guides/consumer-startup-acceptance.md`
- `docs/guides/production-operations.md`
- `docs/guides/readiness-and-maintenance.md`
- `docs/guides/deployment.md`
- `docs/guides/http-assets.md`

Changed or added fixture files:

- `tests/fixtures/consumer/tooling/commands.go`
- `tests/fixtures/consumer/tooling/guide_recipes_test.go`
- `tests/fixtures/consumer/tooling/readiness.go`
- `tests/fixtures/consumer/bootstrap/redis_inventory_test.go`
- `tests/fixtures/consumer/configuredprofile/settings.go`
- `tests/fixtures/consumer/configuredprofile/cmd/profile/main.go`
- `tests/fixtures/consumer/httpassets/assets.go`
- `tests/fixtures/consumer/httpassets/runtime.go`
- `tests/fixtures/consumer/httpassets/portal_runtime_foundry.gen.go`
- `tests/fixtures/consumer/httpassets/.foundry-gen.json`

This evidence file is also new. Framework implementation, CLI/checker/renderer
sources, Makefile, CI, changelog and master status were not changed.

## Commands and lead handoff

Ran `gofmt` on changed handwritten Go files. Generated the new runtime DTO with:

```sh
go run ./cmd/foundry generate --dir ./tests/fixtures/consumer/httpassets
```

The initial generator attempt could not access the native build cache inside the
sandbox. The authorized retry used the existing cache and generated one file.
The guide renderer ran in write mode for these nine guides. Its first attempts
rejected Agent entry placement and punctuation; both authoring mistakes were
corrected before successful rendering. No tests, build commands, verification
gates, service I/O or app exports ran. Compilation and runtime proof are pending
the lead checkpoint; generation and rendering are not substitutes for them.

The lead can use the existing native environment for the combined checkpoint:

```sh
make generate
make generate-check
make docs-check
make fixture-check
```

A focused HTTP recipe rerun, if needed after a checkpoint failure:

```sh
(cd tests/fixtures/consumer && go test -count=1 ./tooling -run '^TestConfiguredHTTPKernel$')
```

Use the shared milestone gate once after lane integration. Existing fixture-check
covers the compiled helper inputs and consumer modules, including the unchanged
TypeScript proof. Deployment host/container acceptance remains its existing
owner's separate gate; no host behavior changed here. Do not retire application
teaching until the lead establishes the replacement proof on the combined source.

## Lead follow-up: ambiguous retirement call shapes

The lead reports that `make generate-check` passed before this follow-up and
`make docs-check` rejected `productionprofile.Check()` in the rendered model
scaffolding example. That fixture package function takes no arguments. The
retired `validation.Rule.Check` method took three. The guide and its source
region remain unchanged by this fix.

With the lead's explicit authorization to edit `internal/retirement`, unknown
receivers now require a matching fixed argument count before a call is reported
as ambiguous. The argument counts live alongside the existing document rules.
Known retired owners still fail regardless of argument count. Bare method
references still require owner evidence. Parenthesized and generic call targets
use the same matching path.

The sibling review covered all methods selected by this owner-aware rule:

- `Check` requires three arguments, `ForUpdate` zero, and `WithScopes` and
  `WithAuthorization` one each. An ellipsis cannot match these fixed signatures.
- The retired `Values().Entries()` chain requires a zero-argument `Values` call
  and, for an unknown owner, a zero-argument `Entries` call.
- `Rules`, `WithPermissions`, `WithBodyValidation` and `WithMiddleware` are
  variadic, so argument counts cannot distinguish their valid call forms.
  Their existing owner requirements remain, with regression cases for empty or
  spread arguments as applicable.
- `.Validate` has no bare-name ambiguous-receiver rule. Its owner-qualified
  retirement checks remain. Tests preserve unrelated `settings.Validate()` and
  `settings.Validate(ctx)` calls.

Added `TestDocumentAmbiguousCallShapes` for the reported zero-argument package
function, unqualified retired three-argument call, current `validation.Check`,
fixed-arity sibling methods, retained package functions, method references and
chain shapes. The migration-label boundary test now uses the actual retired
three-argument call rather than the unrelated one-argument spelling.

Follow-up files:

- `internal/retirement/document_owners.go`
- `internal/retirement/document_rules.json`
- `internal/retirement/document_owners_test.go`
- `internal/retirement/docs_test.go`
- This evidence record

Go files were formatted and the source diff was reviewed. No tests, renderer,
generation, build or verification commands ran during this follow-up. The lead
can run `go test ./internal/retirement` and `make docs-check` at its checkpoint.
The earlier generation pass does not verify this retirement-rule change.
