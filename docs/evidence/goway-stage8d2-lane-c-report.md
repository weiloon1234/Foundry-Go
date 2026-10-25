# Stage 8d2 — lane C behavior ports

Base: `e7b4d8a4`. No public API or legacy engine removed. Generator sources,
generated output, oldmodels/oldreporting and the migration guide are untouched.

## Frozen comparisons

The shared format/validator already lives at root `tests/oracle`; no move or copy
was necessary. Replay-only assertions now reject capture mode and pending/missing
records, preserve occurrence IDs and enforce selected-test consumption.

- [Framework capture](../../tests/oracle/testdata/stage8d/README.md): 41 actual legacy
  observations, including report SQL, bounded record streams, keyed writes,
  extension batches, identities/tokens, column signatures and retention bindings.
- [Matching capture](../../tests/oracle/testdata/stage8d-matching/README.md): 12 actual
  legacy success/error classifications. Separate provenance preserves the earlier
  capture's source manifest. Both use the existing receipt/validator; instrumented
  source text and exact commands are retained.
- Existing Stage 8c extension SQL/predicate and nested-binding records remain
  unchanged. Their native assertions replay the same sites and occurrence IDs.

## Old → native anchors

| Old assertion | Native replacement |
| --- | --- |
| `datatable/TestDatatableCutoverProjectionGolden` live legacy projection | Same test, `rows.Compile`, frozen SQL/bindings for all five requests. |
| `database/TestRecordIterationLegacyParity` legacy iterator | Same frozen observation IDs, `rows.CompileRead/PreflightRead/EachRead`; all ten shapes, batching, duplicate/callback behavior remain live. `TestRecordIterationFailuresReleasePoolAndSession` retains all twelve failure modes across three shapes and both owners. |
| `orm/TestScopedKeyedWriteParity` Patch/Remove branch | `Update`/`Delete`; ten frozen SQL/hook/result/outcome traces, same live scoped-key and statement-count checks. |
| `orm/TestExtensionAdapterBatchAndMissingParity`, `TestIdentityAdapterParity` | Native `FoundryExtension`, `IdentityOf`, loading and key/lock operations against frozen batches, identities and SQL. Invalid bindings, missing/loaded state, mutation ownership and zero capabilities stay executable. |
| Pagination/plan alias comparisons | Native-to-querycore identity, bytes, bounds and error identity. No legacy import. |
| `orm/TestColumnKindCoreParity` old-field reflection | Native reflection against the six frozen legacy signature maps; field value/nullability/results preserved. Package-function operations still forbidden on columns. |
| Predicate/statement/cursor/plan retention | Native wrappers, scoped/HAVING/negated predicates, joins, annotated results, global scopes, binary snapshots and both cursor directions. SQL/binding comparisons replay legacy records. |
| Legacy Mutation/MutationValues inspection | `TestWriteDraftRetentionAndExplicitInput` checks an actual Creating callback, redacted nested drafts, exact stored/distinct input values and callback veto before SQL. |
| Observer/autocommit catchers | Native read/write calls preserve zero-capability, no-app-callback and exact transaction/SQL counts after removing only duplicate legacy calls. |
| Goway mutator/nested-binding/extension cases | Native model writes, `ThroughRelation`, manager matching and typed invalid identities; cause identity, redaction, no-I/O, authority, closure, classification and loaded-state assertions preserved. |
| Surface and layout self-tests | Native function-field/cursor probes and injected rows/expr layouts. The actual sweep still includes the retained legacy engine until 8d3. |
| Additional `TestScopeSourceFailureNeverReachesSQL` | Native scope failure retained across every terminal, with panic/Goexit, no SQL, no retry and redaction checks. Cursor/routing tests use native contract spellings. |

## Coordination and remaining callers

- `database/orm/transaction_phase_test.go` deliberately still imports the legacy
  package through go/types and retains its existing conversion exceptions while the
  engine remains. Lane A removes only obsolete pairs at 8d3.
- `database/orm/adjustment_lifecycle_test.go` still executes the legacy B39 reference
  path. Decision 5 assigns B39 to lane D; this file is untouched.
- ORM production legacy bridges and relation contract imports remain for 8d1/8d3.
  The rows result-test alias imports are lane D's assigned work.
- `database/record_iteration_test.go` retains shared `retrievalFixtureRecord`, driver
  and observer helpers owned by lane B's retrieval-dispatch test migration; it no
  longer calls the legacy definition or terminal helpers.
- Fixture `comparisons.json` updates only hashes of five reviewed owned sources.
  Replay-only sites cannot participate in a new legacy capture. Integrate after
  lane A's authorized 8c freeze, as planned; native output cannot refill an oracle.

No legacy-only behavioral invariant was intentionally dropped. Legacy package
identity legs and invalid legacy adapters are replaced by canonical-owner identity
and invalid native-adapter assertions, under decisions 2 and 4.

## Acceptance

Capture commands passed before removing their legacy call legs. Targeted root and
fixture builds/vet passed. Race coverage passed for database, ORM, datatable, the
shared oracle package and publisher, the ten touched goway tests and the fixture
freezer. The ORM run is segmented: the initial surface-probe failure was corrected
and rerun; other passing tests retain their results. The convertibility sweep ran.

The datatable suite ran with sourced `.env.test` and
`FOUNDRY_TEST_POSTGRES_REQUIRED=1`; PostgreSQL was reachable and its live tests
passed. The comparison captures and remaining owned tests use controlled drivers.
Both new bundle validators, archived source hashes, replay rejection tests and
existing fixture inventory/replay checks passed. Catalogue: 587 legacy rows mapped
to 579 native cases. `make docs-check`, gofmt and `git diff --check` passed.

Exact commands, segmented results and source hashes are in
[the acceptance evidence](goway-stage8d2-lane-c.json). No footprint probe, budget,
production public method or snapshot changed, so footprint was unaffected and was
not rerun. No performance claim is made.

## Docs notes

Stage 8d2 changes verification consumers only. Public retirement belongs to 8d3.
Historical records are legacy observations, not live old/new comparisons. Keep the
capture/replay distinction in integration notes and the eventual removal report.
