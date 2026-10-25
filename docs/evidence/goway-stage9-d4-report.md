# Stage 9 D4: rebase, nested bindings and root harness

Base: `79db1cf8`. No commit. Completed targeted checks used r9; the interrupted
matching race segment was restarted on r10. Every subsequent Go command uses r10,
with `GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`. This report covers the new increment; the accepted
matching/AddFilesTo/redaction delta remains in the worktree.

## Conflict resolution

All eleven named conflicts are resolved. Integrated evidence and multifactor's
conflicting test keep the base. Articles keeps the base's relation comments;
profiles retains the matching visibility regression. The catalogue, compiler
registry and footprint map are unioned. Transaction catalogue rows keep the
base's newer migration notes; the matching row keeps its ported disposition.

The base's `RecordBatches` and `relationExpressionPredicate` owners are retained.
Their comparison/predicate constructors now use the accepted opaque holders.
The new compiler-binding regression uses the same holder. No redaction guarantee,
validated capability or public method was removed.

## Publish-ready and migrated consumers

`nestedbindings` publishes `Teams`, `ProjectRecords`, `Tasks`, `TeamProjects`,
`ProjectTeam`, `ProjectTasks` and `Tables()`. `ProjectRecords` avoids the existing
`Projects` resolver name. Route resolvers and PostgreSQL test sources now use ORM
keys/columns and direct relations, preserving alternate-key uniqueness, parent
scopes, soft deletion, eager loading, retrieval vetoes and exact statement counts.
The declaration/SQL-parity test passes. Lane A can use `ProjectRecords.Slug` and
`Teams.Name` in `nested_field_owner`; this lane did not edit A's row.

Root handwritten consumers now use ORM for reads, writes, pagination, codecs,
changes, observer injection and query benchmarks. The observer fixture now tests
automatic dispatch with two registered ORM observers, including dependency
construction order and returned stored defaults. The package graph asserts typed
cross-package declaration/assignment use. Enum tests and the module-pinned CLI's
retained config-generation check remain.

Editor probes for the migrated root APIs, nested binding and extension handles
now point to their handwritten/framework definitions. The new ordinary anchor
check uses the same catalogue as agent-smoke, so stale anchors fail without a
real language server. Existing probes for consumers awaiting other lanes remain.
No public framework API, method-surface snapshot or new catalogue row is added.

## Remaining D4 blocker

The root package is **partially migrated**, not ready for generator cleanup.
`model_relations_postgres_test.go`, `model_through_postgres_test.go` and
`model_aggregates_postgres_test.go` remain byte-identical to the base.

They require custom limits on loading existing parents. Legacy
`q.WithRelationLimits(limits).With(...).Load/LoadMissing(...)` carries BatchSize,
MaxRows and MaxDepth. ORM's package functions accept parents and descriptors but
cannot receive those limits; `WithRelationLimits` only accepts a query, which
cannot be passed to either loader. This blocks exact duplicate-parent attachment
budgets, over-budget rejection and existing-parent batch accounting. Lane A owns
`database/orm/relation_tree.go`: publish a typed package-function path for custom
limits on both loaders, without widening Query/Table. The tests are retained
intact rather than replacing these assertions.

Generated directives, generated helpers, output and manifests remain until all
inbound legacy consumers are gone and the lead runs the generation barrier.

## Footprint and contracts

The four original probe measurements were frozen before edits in
`goway-stage9-d4-legacy-footprint.json`, including source hashes. `model_type`,
`model_isolated` and `endpoint_plain` keep their source and ceilings.

The replacement `model_assignments` probe (existing `model_draft` directory) checks
an ordinary typed assignment list. It measures **233 functions / 1,647 symbols /
1,060,408 bytes**, versus legacy **225 / 1,569 / 999,256**. In this workload, switching to the typed assignment carrier adds eight
functions, within the old budget. The new ceilings are **268 / 1,895 / 1,219,470**, each ceil(measured × 1.15),
and all below the old ceilings. Legacy values are not relabelled as ORM results.

All 143 Go-way footprint probes pass: `q_with` remains **1,211/1,211** and
`aggregate_families` **4,365/4,369**. The registry union retains the base's ceilings.
No generated output, module pin, Makefile, manifest, schema tag or TypeScript
artifact changed. Existing `make typescript-check` already selects both real
HTTP/WebSocket client and React/Vue adapter tests in `clientcontracts`; no wiring
change is needed. Clientcontracts remains lane B's package.

## Acceptance and shared files

Build, database/core/editor vet, consumer/Go-way vet, core races, consumer races,
editor unit/race tests, anchor validation, matching/AddFilesTo regressions, all
840 Go-way compiler cases, the consumer matching negative, all 143 Go-way probes,
the four consumer probes, catalogue, docs-check, gofmt and diff-check pass.
Exact commands, logs and hashes are in `goway-stage9-d4.json`.

The first broad attachments race command hit the sandbox's loopback-listener
restriction in a PostgreSQL test before its opt-in check. The targeted rerun
excludes PostgreSQL tests and passes. The first vet caught the rebased
compiler-binding literal, fixed at its constructor. Initial fixture compilation
caught the loader gap above and old terminal names. All final applicable checks
pass. Live PostgreSQL, TypeScript and real gopls are lead integration gates, not
claimed here. No generation was run, per the lead-owned barrier.

Shared files touched/resolved in this increment: the compile-fail mapping,
Go-way compiler and footprint registries, ORM projection bridge and relation
scope, shared record iteration, compiler-binding test, consumer README and
footprint harness, and both editor acceptance files. The base versions of
`read_lifecycle_chunks.go`, D8/D1 evidence and multifactor's conflicting test were
restored. All other new edits are lane D's root and nestedbindings consumers.

## Docs notes

Keep the generator retirement status pending the existing-parent loading-limit
adapter and other lanes' remaining consumers. The consumer README documents the
handwritten declarations, frozen baseline, remaining oracle files and unchanged
TypeScript gate. Lead-owned master docs and changelog are untouched.
