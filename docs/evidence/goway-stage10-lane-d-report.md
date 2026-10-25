# Stage 10 lane D: guides and editor coverage

Base: `54508185`. No commit. No framework runtime, public method snapshot,
compile-fail catalogue, footprint ceiling, persisted schema or wire contract changes.

## Guides and examples

Updated 24 guides covering stored identity and attribution; extension owners,
translation, metadata and attachment slots; authentication and credential stores;
model binding; model and HTTP pagination; factories and database assertions;
validation/database; model scaffolds; countries, notifications and editor tooling.
Each links the ORM migration guide. DTO, configuration, enum and transport
contract generation remains documented where it still applies.

The 47 marked Go excerpts match compiled source exactly. The new
`tests/fixtures/goway/guideadapters` package covers identity round trips,
authentication identity adapters, pagination, validation scopes/exclusions,
assignment factories, typed-role scaffolds and valid extension registrations.
It also checks declarations and prevents snippet drift. `TestGuideSnippets` consumes
`testkit.TrackExternalInputs` so make-driven test caching includes guide and
external-source changes; direct snippet acceptance uses `-count=1`. Existing examples are
reused from profiles, articles, httpmodels, nestedbindings, httppagination,
httpvalidation, authenticating, notifying and passwords. Their test packages
compile without running PostgreSQL tests.

## Editor and generated documentation

All editor probes remain. Three raw-field documentation probes now assert
handwritten comments describing stored identity, explicit getters, persistence-time
mutators and direct assignment. Nullable-only clearing and the localized article
heading documentation assertions are unchanged. The query-definition failure
message now names the handwritten ORM method.

The `tooling-factory-draft`, `orm-report-compile` and `combined-record-fields`
probes now anchor in separate `editor_examples_test.go` files in tooling,
reporting and setqueries. Their modern expressions and every completion, hover
and definition assertion are unchanged. The set-report builder moved unchanged
into its example file, retaining the existing PostgreSQL and oracle callers.
None of these examples depend on legacy query APIs or legacy oracle helpers;
no existing tests or probes were deleted.

Shared files touched:

- `internal/agent/field_documentation_acceptance_test.go`
- `internal/agent/gopls_acceptance_test.go`
- `tests/fixtures/consumer/tooling/editor_examples_test.go`
- `tests/fixtures/consumer/reporting/editor_examples_test.go`
- `tests/fixtures/consumer/setqueries/editor_examples_test.go`
- `tests/fixtures/consumer/setqueries/derived_aggregate_postgres_test.go` (builder moved unchanged)
- `tests/fixtures/consumer/mutatorqueries/member.go` (comments only)
- `tests/fixtures/consumer/mutatorqueries/member_foundry.gen.go` (regenerated comments)
- `tests/fixtures/consumer/mutatorqueries/.foundry-gen.json` (generator-owned hash)

The first editor-gate attempt stopped at generated-output freshness, before any
probe ran. The new handwritten comments changed the retained generator's copied
comments. `make generate GENERATION_DIRS=tests/fixtures/consumer/mutatorqueries`
refreshed them and their ownership manifest. It removed no files. No generator
source was edited. Lane A can retire these generated notices and artifacts in 8c;
the rewritten probes require the handwritten comments that remain.

## Acceptance

Review-fix acceptance passed on r14: `make agent-smoke` (all 502 probes in one
run), `make docs-check`, the snippet test with `-count=1`, and the targeted
consumer example check. The factory example compiles; the two report examples
execute query compilation. Formatting and `git diff --check` also passed.

The earlier pre-review editor run passed in segments after four gopls shutdown
failures. Its commands remain in the evidence history; the fresh full run above
verifies the final anchors without reruns or weakened assertions.

Exact commands, outcomes and final source hashes are recorded in
[the evidence JSON](goway-stage10-lane-d.json). Every Go command uses
`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r14 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
The editor gate selects the approved gopls at
`/Users/weiloon/Projects/rust-go/Foundry-Go/bin/gopls`.

No PostgreSQL or TypeScript execution was needed for this documentation slice;
transport declarations and exported contracts were not changed.

## Stale content outside this rewrite

The base still has legacy instructions in `model-generation.md`, `model-queries.md`,
`model-relations.md` and related relationship guides. Lane A's emitter removal
and the lead's shared-guide integration need to reconcile these. Lane B/C-owned
write/lifecycle and expression/report guides also contain the expected legacy
examples pending their parallel rewrites.

Shared integration guides still need follow-up: `idempotent-operations.md` uses
`QueryOrders`/`OrderDraft`; `access-scopes.md` and `websocket-distributed.md` use
`FoundryReference`, and `websocket.md` describes that generated method. Encryption,
database routing and query-build resource guides also mention generated models.
The migration guide deliberately retains old examples for comparison and is not
counted as stale content.

## Docs notes

Suggested lead-owned status text: Stage 10 lane D documents handwritten model
identity, extension slots and their adapters, auth persistence, model binding,
pagination, factories, database validation and scaffolds. Editor probes retain
completion, hover and definition coverage using handwritten behavior comments.
Link this report and its current-source acceptance evidence. Keep generator
retirement status with lane A's Stage 8c result.

Removed `--field-docs` guidance from model scaffolding and developer tooling.
Lane A retires that generated model-field notice flag in Stage 8c; this slice
changes its documentation only.
