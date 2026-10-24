# Stage 8a, lane D

## Implementation

Resolved the rebase conflicts by retaining f3cfffd's integrated 3a code/evidence and unioning the registries, public snapshot, compiler cases and footprint probes. Adapted the reapplied 3b declarations to CreatedAt/UpdatedAt and natural lookup-key carriers; the validation fixture uses SoftDelete. Single timestamps that were caller-supplied or database-defaulted remain ordinary Time fields. No new force-delete capability was needed.

The modern attachment, translation and metadata handles now use private typed state and `internal/extensionslot`, whose batch engine is shared non-generic state. They no longer construct legacy ExtensionSlot, ExtensionBinding or SlotFetch descriptors. Concrete model/value accessors remain typed at construction. ORM identities and matching-key helpers no longer retain legacy predicates; legacy conversion happens at explicit compatibility entry points. Managers, storage identity codecs, reconciliation and SQL behavior remain shared.

The legacy public entry points remain callable through compatibility files. They delegate to the same loader and managers. The shared identity batch bound remains 1,000. Loading retains executor affinity, missing-slot selection, fetched/attached budgets, batch sizes, deferred attachment and shallow snapshots when a fetch reuses its output buffer.

## Surface and catalogue

Added only two framework bridge package functions in 8a: `orm.FoundryExtension` and `query.FoundryExtensionSlot`. Both require a validated QuerySeal and concrete framework-owned state; neither passes capabilities to callbacks. No Query/Table methods were added. The ORM snapshot is additive; it also retains the reapplied validation ValueLookup and rows.Each surface. Legacy methods remain available.

No new catalogue rows in 8a. The union retains the extension owner/key/value/runtime restrictions and five validation rows. New runtime regressions compare legacy, compatibility and native loading, invalid capabilities and slot kinds, exact batch selection, budget failure without attachment, malformed/cancelled fetches and reused fetch buffers.

## Footprint

The matched bound-extension archive probes measure 2,083 modern functions versus 9,733 legacy functions (Go 1.27.1, darwin/arm64). The modern path remains under one quarter of the legacy path and under its unchanged 2,478 ceiling. `q_with` remains 1,211/1,211 and aggregate families 4,366/4,369. These are emitted archive-symbol counts, not runtime speed claims. Legacy forwarding adapters increase the legacy workload. No existing ceiling was raised.

## Verification

Acceptance results and current source fingerprints are recorded in [the evidence JSON](goway-stage8a-lane-d.json). The interrupted vet segment was restarted on r5; completed rebase results and the completed pre-rotation build remain valid. No live PostgreSQL was run, per the lane brief.

All requested commands passed:

| Command / segment | Result |
| --- | --- |
| `go build ./...` | PASS before rotation; unchanged runtime source |
| `go vet ./database/... ./internal/...` plus touched consumer packages | PASS on r5 |
| Segmented `go test -race` for ORM/query/core/shared loader, stores and consumers | PASS on r5 |
| Footprint probes; `make fixture-check` (vet and tests) | PASS on r5 |
| `python3 tools/goway_catalogue.py` | PASS: 512 rows, 504 distinct Go-way cases |
| `make generate`; `make generate-check` | PASS: no generated changes |
| `make typescript-check` | PASS |
| `make docs-check`; `make fmt-check`; `git diff --check` | PASS |
| `make agent-smoke`, approved gopls v0.23.0 | PASS: 466 probes, 387.013 seconds |

The earlier rebase pass exposed snapshot ordering and conversion between ValueLookup and KeyLookup. The registry ordering and private ValueLookup pointer-field brand were corrected; targeted races passed, followed by this full passing gate. The first TypeScript invocation lacked explicit tool paths; the configured rerun and final gate passed. The cache-rotation interruption was resumed at vet on r5. No final-gate test failure required a code change.

## Shared files

The complete changed-file inventory and hashes are in the evidence JSON. Shared registry files touched: `database/orm/README.md`, `database/orm/method_surface_test.go`, `database/orm/testdata/public-methods.txt`, `tests/fixtures/goway/compiler_cases_test.go`, `tests/fixtures/goway/footprint_test.go`, and the reapplied validation rows in `blueprint/goway-data-layer/compilefail-mapping.tsv`. `internal/agent/gopls_acceptance_test.go` retains B34 and follows the moved compatibility definitions. Shared runtime changes are in ORM identity/extension adapters, legacy query identity/extension adapters, the new internal/extensionslot owner and internal/querycore identity bound. The integrated 3a evidence is unchanged.

## Docs notes

For lead-owned milestone docs: Stage 8a lane D moves modern extension loading and identity/predicate backing off legacy model-query descriptors. Public legacy adapters remain until consumers migrate; this does not retire model generation. The next consumer slice can delete compatibility files only after generated consumers and old-API evidence have an agreed replacement. Lane C's reviewed rows.Each remains the integration choice; this worktree retains its self-contained duplicate file for clean removal.
