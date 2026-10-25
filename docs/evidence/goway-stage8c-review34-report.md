# Oracle prerequisites: review 34

Both findings are addressed without changing production code or frozen records.

- `TestMutatorErrorRedactionInBothAPIs` is now in `mutators_behavior_test.go`.
  Its legacy branch declares its model, input field, mutator and query through the
  retained runtime API. Both branches still assert errors.As/Is cause identity,
  redaction through Error, %v, %+v and %#v, and zero SQL statements.
- The import guard parses every Go import without a text prefilter. Files that
  import legacy models may contain only recorded test entry points and supporting
  declarations: an unrecorded test, benchmark, fuzz target, example or init hook
  fails even when another test in the file is recorded. Separate behavior files
  contain no generated-model imports. Regressions cover the mixed-file bypass,
  escaped import paths, ordinary external dependencies and descriptor strings.

Fifteen ordinary tests and one benchmark moved out of nine recording files.
Their assertions are unchanged except the redaction test's handwritten legacy
declaration and the statement-detector assertions naming their new source file.
The scope/exclusion test now checks raw SQL and exact typed bindings instead of
searching the oracle's structured JSON transcript:

| Behavior file | Tests |
| --- | --- |
| `tests/fixtures/goway/extension_handles_behavior_test.go` | `TestExtensionHandlesDoNotExposeLegacyFamilies`, `TestZeroExtensionHandlesFailClosed`, `TestThinExtensionBindingKeepsUnboundDeclarationImmutable` |
| `tests/fixtures/goway/integration_adapters_behavior_test.go` | `TestORMExtensionDeclaration` |
| `tests/fixtures/goway/mutators_behavior_test.go` | `TestMutatorInputsAreReusableAndOwned`, `TestMutatorErrorRedactionInBothAPIs` |
| `tests/fixtures/goway/relation_filter_behavior_test.go` | `TestRelationFilterRejectsForeignMetadata` |
| `tests/fixtures/goway/relation_writes_behavior_test.go` | `TestRelationWriteMorphDefaultsAndSingleStatements` |
| `tests/fixtures/goway/report_projection_behavior_test.go` | `TestReportRelationAdapterRejectsForeignDeclaration`, `TestReportEnumPatternCompilerDefense` |
| `tests/fixtures/goway/retrieval_scope_behavior_test.go` | `TestScopePropagationProjectionJoinsAggregatesAndRelationBypasses`, `TestJoinedRetrievalCallbacksVisitOnlyCurrentBatch` |
| `tests/fixtures/goway/statement_detector_behavior_test.go` | `TestStatementDetectorExternalSitesAndBatchLengths`, `BenchmarkStatementDetectorConsumer` |
| `tests/fixtures/goway/validation_database_behavior_test.go` | `TestValidationDatabaseRejectsBadSourcesAndRedacts`, `TestValidationDatabaseCheckTimeScopesAndExclusions` |

The remaining importer inventory is still 41 files; the complete list is in
[the prerequisite report](goway-stage8c-postgres-prerequisites-report.md).
The census is now 500 test sources, 84 paired entry points and 119 full comparison
sites (117 offline). No SQL/result expectations, public surfaces, ceilings,
production sources or generated outputs changed. Final freeze and PostgreSQL
execution remain with the lead.

## Acceptance

Passed targeted acceptance (r13, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off):

- Recorder race tests, including mixed-file and escaped-import regressions; the
  import audit and full/offline plan checks were rerun after the census updates.
- All 30 affected fixture tests under race, in a segmented run: 29 passed, then
  the repaired scope/exclusion test passed on its focused rerun. Both legacy and
  ORM redaction branches preserve cause identity and stop before SQL.
- `go vet . ./cmd/freeze-oracles`, catalogue validation, formatting and diff checks.

The guard initially caught a remaining ordinary benchmark; it was moved too.
Compilation caught the generated convenience-method name in the handwritten test
and two unused imports; these were corrected to the underlying filtered Patch call.
The runtime run exposed an old plain-text assertion against structured oracle JSON;
its replacement checks raw SQL predicates and exact typed scope/exclusion bindings.
No SQL expectation or record was weakened or rewritten.

Exact commands, logs, documentation-check result and source hashes are in
`goway-stage8c-review34.json`. PostgreSQL, the final freeze and benchmark measurement
were not run. No commit.

## Docs notes

Generator retirement can retain ordinary redaction and behavior coverage without
generated model declarations. The recording boundary now requires separate files,
instead of granting permission to an entire mixed file. Lead-owned docs unchanged.
