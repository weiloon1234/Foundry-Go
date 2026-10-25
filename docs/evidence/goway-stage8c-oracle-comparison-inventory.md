# Oracle comparison census

The reviewed capture census covers the fixture modules, not every framework test.
It locks all fixture test-source hashes and names paired entry points independently
of recorder discovery. The current census has 489 sources and 84 paired entry points.

| Source | Paired entry points |
| --- | --- |
| `consumer/articles/tables_test.go` | `TestORMExtensionFoundationParity`, `TestORMFoundationParity` |
| `consumer/bootstrap/tables_test.go` | `TestORMActorDeclarationParity` |
| `consumer/catalog/orm_test.go` | `TestProductORMParity` |
| `consumer/comparisonqueries/scalar_clause_parity_test.go` | `TestAliasedScalarWindowSQLParity` |
| `consumer/correlations/sql_test.go` | `TestORMCorrelatedGroupingAndRelationParity` |
| `consumer/distinctqueries/correlated_parity_test.go` | `TestCorrelatedDistinctOnSQLParity` |
| `consumer/httpmodels/sql_parity_test.go` | `TestDeclaredBindingsPreserveLegacySQL` |
| `consumer/intervalqueries/orm_test.go` | `TestIntervalDeclarationSQLParity` |
| `consumer/jsonqueries/relation_filter_test.go` | `TestJSONNaturalKeyRelationFilterParity` |
| `consumer/lateralqueries/clause_parity_test.go` | `TestCorrelatedRecordClauseSQLParity` |
| `consumer/linkqueries/orm_test.go` | `TestORMDeclarationsAndReadParity` |
| `consumer/models/orm_postgres_test.go` | `TestPostgresSharedDeclarationRelationParity` (PostgreSQL) |
| `consumer/models/orm_test.go` | `TestORMDeclarationSQLParity` |
| `consumer/multifactor/tables_test.go` | `TestORMFoundationParity` |
| `consumer/officequeries/related_value_test.go` | `TestOfficeRelatedValueSQLParity` |
| `consumer/officequeries/tables_test.go` | `TestORMOfficeDeclarationParity` |
| `consumer/passwords/tables_test.go` | `TestORMFoundationParity` |
| `consumer/pivothooks/tables_test.go` | `TestORMPivotDeclarationParity` |
| `consumer/profiles/tables_test.go` | `TestORMFoundationParity`, `TestORMProfileExtensionScopeAndVisibilityParity` |
| `consumer/projectionqueries/aggregate_contracts_test.go` | `TestFilteredAggregatePhaseSQLParity` |
| `consumer/reporting/orm_test.go` | `TestOperatorORMIdentityAndLookupParity` |
| `consumer/reporting/source_golden_test.go` | `TestReportSourceGolden` |
| `consumer/reports/rows_test.go` | `TestRowsSQLParity` |
| `consumer/temporalqueries/orm_test.go` | `TestTemporalDeclarationSQLParity` |
| `consumer/tooling/database_test.go` | `TestUserPlanAndRoutingSQLParity` |
| `consumer/transactionqueries/orm_test.go` | `TestTransactionDeclarationSQLParity` |
| `consumer/windowqueries/orm_test.go` | `TestWindowReportSQLParity` |
| `goway/adjustments_golden_test.go` | `TestAdjustmentsGolden` |
| `goway/aggregate_golden_test.go` | `TestStage6a3AggregateGolden` |
| `goway/audit_golden_test.go` | `TestAuditOldAndNewAgree` |
| `goway/composition_golden_test.go` | `TestStage6c2CompositionGolden` |
| `goway/conditional_golden_test.go` | `TestStage6a2ConditionalGolden` |
| `goway/conflict_advanced_golden_test.go` | `TestAdvancedConflictOldAndNewAgree` |
| `goway/conflict_golden_test.go` | `TestConflictOldAndNewAgree` |
| `goway/correlation_lateral_golden_test.go` | `TestStage6b3CorrelationAndLateralGolden` |
| `goway/expression_golden_test.go` | `TestStage6a1ExpressionGolden` |
| `goway/extension_handles_test.go` | `TestThinExtensionHandlesPreserveLegacyPoliciesAndUnboundBehavior` |
| `goway/golden_test.go` | `TestOldAndNewDataLayersAgree` |
| `goway/integration_adapters_test.go` | `TestFactoryORMAndGeneratedParity`, `TestORMModelBindingScopeAndDuplicateParity` |
| `goway/integrations/pagination_test.go` | `TestORMPaginationKeepsTransportIdentity` |
| `goway/joins_golden_test.go` | `TestStage6b1JoinGolden` |
| `goway/json_path_golden_test.go` | `TestStage6d3JSONGolden` |
| `goway/lifecycle_golden_test.go` | `TestLifecycleOldAndNewAgree` |
| `goway/load_query_golden_test.go` | `TestLoadFromQuerySQLMatchesLegacy` |
| `goway/mutators_golden_test.go` | `TestInputPrimaryKeyOldAndNewAgree`, `TestMutatorsOldAndNewAgree` |
| `goway/nested_binding_orm_test.go` | `TestNestedORMBindingHasNoGlobalFallback` |
| `goway/on_conditions_golden_test.go` | `TestStage6b2OnGolden` |
| `goway/pgsmoke/mutators_test.go` | `TestMutators` (PostgreSQL) |
| `goway/read_terminal_golden_test.go` | `TestStage9ReadCTEScalarAndValueSetParity`, `TestStage9ReadScalarCompileParity` |
| `goway/reads_golden_test.go` | `TestReadSQLAndCursorTokensAgree` |
| `goway/records_golden_test.go` | `TestStage6c1RecordGolden` |
| `goway/relation_aggregates_golden_test.go` | `TestRelationAggregateGoldens` |
| `goway/relation_filter_test.go` | `TestRelationExpressionFilterSQLParity` |
| `goway/relation_writes_test.go` | `TestRelationWriteGoldens` |
| `goway/relations_golden_test.go` | `TestDirectRelationSQLMatchesOldAPI` |
| `goway/relations_joined_golden_test.go` | `TestJoinedExistenceSQLMatchesOldAPI`, `TestJoinedFamiliesSQLMatchesOldAPI` |
| `goway/report_projection_golden_test.go` | `TestReportFilterAdaptersGolden`, `TestReportProjectionGolden` |
| `goway/result_read_golden_test.go` | `TestStage6c3ResultGolden` |
| `goway/retrieval_scope_golden_test.go` | `TestRetrievalAndScopeOldAndNewAgree` |
| `goway/scalar_golden_test.go` | `TestStage6d2ScalarGolden` |
| `goway/sensitive_golden_test.go` | `TestPasswordOldAndNewAgree`, `TestSensitiveOldAndNewAgree`, `TestSensitiveStreamingFailureParity` |
| `goway/source_write_golden_test.go` | `TestSourceWriteOldAndNewAgree` |
| `goway/stage7_remainder_golden_test.go` | `TestStage7CursorMappingSQLParity`, `TestStage7SeederSQLParity` |
| `goway/statement_detector_test.go` | `TestCompiledStatementsCarryTrustedShapes`, `TestCursorStatementsCarryTrustedShapes` |
| `goway/store_capabilities_golden_test.go` | `TestStoreEachLifecycleGolden`, `TestStoreUUIDSeekGolden` |
| `goway/subqueries_golden_test.go` | `TestStage6b2SubqueryGolden` |
| `goway/terminal_writes_golden_test.go` | `TestTerminalWritesGolden` |
| `goway/transaction_composition_golden_test.go` | `TestStage6b4TransactionCompositionGolden` |
| `goway/validation_database_test.go` | `TestValidationDatabaseGlobalScopesWithoutRetrieval`, `TestValidationDatabaseObservationParity`, `TestValidationDatabaseRulesAndDescriptions` |
| `goway/window_golden_test.go` | `TestStage6d1WindowGolden` |

Recording captures actual SQL, typed bindings, hydrated results, cursor transport,
callback traces and observed failure classifications where the original comparison
asserts them. Snapshot normalization preserves scalar storage and boundaries.
New or changed test sources require census review. The AST call graph must match
reviewed entry points, every selected site must execute, and replay must consume
every selected occurrence. The PostgreSQL capture requires the backend; ordinary
`--check` explicitly excludes its two sites and cannot publish.

## Final self-sweep

The sweep examined oldmodels/oldreporting imports, legacy query and ORM imports,
paired-result comparisons, shared comparison helpers and legacy/current branches,
including comparisons inside already-instrumented source files. Besides the two
reviewed omissions, it found and now records the two statement-detector comparisons
and the direct persisted-email comparison in PostgreSQL `TestMutators`.

These live comparisons remain outside the **fixture** capture census and are
reported for the lead's retirement decisions; no complete-tree freeze is claimed:

| Source | Retained comparison |
| --- | --- |
| `datatable/projection_golden_test.go` | `TestDatatableCutoverProjectionGolden`: SQL and bindings |
| `database/record_iteration_test.go` | `TestRecordIterationLegacyParity`: SQL and bindings, alongside lifecycle/batch assertions |
| `database/orm/write_keyed_test.go` | `TestScopedKeyedWriteParity`: keyed-write SQL, hooks and results |
| `database/orm/integration_adapters_test.go` | `TestExtensionAdapterBatchAndMissingParity`, `TestIdentityAdapterParity`: extension results, identities/tokens and SQL |
| `database/orm/pagination_contract_test.go` | `TestPaginationContractsShareOneOwner`, `TestPaginationContractBytesAndRedaction`: type identity, cursor parsing and wire values |
| `database/orm/plan_contract_test.go` | `TestPlanContractsShareOneOwner`: public type identity |
| `database/orm/column_kinds_test.go` | `TestColumnKindCoreParity`: old/new field method signatures |

Additional retained interoperability coverage (not a direct golden comparison):
`goway/pgsmoke/sensitive_test.go:TestSensitivePersistenceAcrossAPIs` writes with one
API and reads with the other; the remaining mutation/read assertions in
`goway/pgsmoke/mutators_test.go:TestMutators` do likewise. They need an explicit
retirement plan as tests, even though the new direct email comparison is captured.
`database/decoding_paths_test.go` exercises both APIs against shared redaction
assertions; `database/record_iteration_test.go` also retains dual-API failure and
resource-release assertions. Observer compatibility tests explicitly reject mixed
legacy/current observer families. These do not become replaceable by a transcript.

`internal/storemigration/stores_test.go`, `stores_3b_test.go` and
`outbox/publisher/store_parity_test.go` already compare against their separately
frozen legacy JSON goldens. They are not missing live legacy capture.
`internal/ormcore/primitive_codec_test.go` compares new adapters to retained codec
implementations; both codec owners survive generator retirement. Other legacy-only
PostgreSQL fixtures are behavior coverage, not paired legacy/current observations.
