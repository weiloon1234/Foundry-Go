# Stage 9 lane C cleanup barrier — first removal

Base `0ada5ac7`. No commit. This is a partial barrier delta: generation was removed where no live SQL/hydration oracle depends on it. All existing positive test assertions remain.

## Removed

| Owner | Directives and generated output removed |
| --- | --- |
| lateralqueries | UserOrder, RankedOrder (2 projections) |
| reports | ReferralRow, UserPair, OrderBuyerRow, ReferralOrderRow, LocationRow, UserOrderStats, ScalarReport (7 projections) |
| transactionqueries | Totals, Pair (2 projections) |
| windowqueries | NavigationRow, WindowTotal (2 projections) |
| recursivequeries | Node (1 model) |

Total: **14 directives (1 model, 13 projections), 14 generated Go files**. `make generate` removed those outputs and refreshed five owning `.foundry-gen.json` manifests. Model/result structs, keys, nullable fields, relation slots and handwritten schemas remain. No DTO, enum, config or contract directive/output changed; the frozen `goway/oldmodels` and `oldreporting` packages were untouched.

Retained `User.CurrentEmailDraft` and `Product.CreatorDraft` byte-identically after the root fixture demonstrated that lane D's two assertions still require them. Their initial removal was reversed; no other owner's tests were edited. Remove them together with D's root/editor cutover. No relation/aggregate factory was removed: the 13 remaining factories are required by retained generated oracle callers.

Ported the consumer negatives `alias_predicate_owner`, `recursive_natural_key` and `scalar_scope` to ORM/expr/rows. They continue rejecting, respectively, a predicate from another alias, a model ID in place of a natural key, and a scalar subquery attached to the wrong input scope. The first and third registry diagnostics were updated for the current API family. These mirror the existing Go-way negatives; no test was deleted or reduced to an unrelated missing-symbol error.

## Blocked removals and exact owners

The base still contains live legacy parity paths, not frozen SQL/binding/hydration records. The prior cleanup audit and Stage 8c decisions require freezing those expectations before deleting them. The lead confirmed that lane A's oracle harness is being finalized; keep these paths until it freezes their expectations. No synthetic or freshly inferred expected results have been substituted.

| Owner | Dependency |
| --- | --- |
| Lane A | `httpmodels/sql_parity_test.go:29,34,38` still calls `models.QueryUsers`, `UserFields`, `QueryCountries`, `QueryOrders`, `OrderFields`. |
| Lane A | `tooling/database_test.go:84–86` still calls `models.QueryUsers` for Explain/read/primary parity. Shared User/Order/Country output removal must wait for these callers; their files were not edited. |
| Lane D (planned concurrent cutover) | `generated_test.go:109` calls `User.CurrentEmailDraft`; `package_graph_test.go:45` calls `Product.CreatorDraft`. Both helpers are restored until these root assertions move to ORM assignments. Root files and editor probes were not edited. |
| Lead oracle freeze + C | C's SQL/hydration parity files listed below retain the other generated APIs and require frozen records. |

The freeze queue below names each remaining legacy API root and legacy query constructor in the 26 C oracle files and two A callers. Paths are relative to `tests/fixtures/consumer/`. [The exact caller ledger](goway-stage9-c-barrier-freeze.tsv) includes file:line for every occurrence. Chained query/field/selection methods remain reached through these roots; the existing test bodies retain the full compilation and hydration assertions.

| Owner | Caller | Exact legacy symbols |
| --- | --- | --- |
| A | `httpmodels/sql_parity_test.go` | `models.OrderFields`, `models.QueryCountries`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields` |
| A | `tooling/database_test.go` | `models.QueryUsers` |
| C | `catalog/orm_test.go` | `catalog.ProductFields`, `catalog.ProductRelations`, `catalog.QueryProducts` |
| C | `comparisonqueries/scalar_clause_parity_test.go` | `models.QueryUsers`, `models.UserFields`, `models.UserFieldsAt`, `query.As`, `query.CTE`, `query.Equal`, `query.NullableRow`, `query.ScalarRowQuery`, `query.SelectValue`, `query.Value` |
| C | `correlations/sql_test.go` | `models.OrderFieldsAt`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields`, `models.UserFieldsAt`, `models.UserRelations`, `query.Correlate`, `query.Count`, `query.Grouped`, `query.InnerScope`, `query.OuterScope`, `query.SelectValue`, `query.Statement` |
| C | `distinctqueries/clause_parity_test.go` | `models.OrderFields`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields`, `query.As`, `query.CTE`, `query.Count`, `query.Equal`, `query.ExistsQuery`, `query.NullableRow`, `query.RecordQuerySource`, `query.ScalarRowQuery`, `query.SelectRecord`, `query.SelectValue`, `query.Statement`, `query.Value`, `query.ValueQuery` |
| C | `distinctqueries/correlated_parity_test.go` | `models.OrderFieldsAt`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFieldsAt`, `query.As`, `query.Correlate`, `query.InnerScope`, `query.OuterScope`, `query.SelectCorrelatedValue`, `query.Value` |
| C | `distinctqueries/record_distinct_on_test.go` | `models.OrderFields`, `models.OrderFieldsAt`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFieldsAt`, `query.As`, `query.InnerJoin`, `query.LeftScope`, `query.On`, `query.RightScope`, `query.SelectRecord` |
| C | `framequeries/sql_parity_test.go` | `framequeries.QueryFrameSamples`, `framequeries.SampleFields`, `query.Count`, `query.CurrentRow`, `query.Param`, `query.Preceding`, `query.RowNumber`, `query.SelectValue`, `query.WindowFor` |
| C | `intervalqueries/orm_test.go` | `intervalqueries.LabelFields`, `intervalqueries.PlanRelations`, `intervalqueries.ProjectSummary`, `intervalqueries.QueryIntervalLabels`, `intervalqueries.QueryIntervalLinks`, `intervalqueries.QueryIntervalPlans`, `intervalqueries.QueryIntervalSamples`, `intervalqueries.SampleFields`, `query.Avg`, `query.Count`, `query.Statement`, `query.Sum` |
| C | `jsonqueries/path_sql_test.go` | `jsonqueries.DocumentFields`, `jsonqueries.QueryJsonDocuments`, `query.SelectValue` |
| C | `jsonqueries/relation_filter_test.go` | `jsonqueries.DocumentRelations`, `jsonqueries.PolicyFields`, `jsonqueries.QueryJsonDocuments`, `query.JSONContains`, `query.Param` |
| C | `jsonqueries/snapshot_parity_test.go` | `jsonqueries.DocumentFields`, `jsonqueries.ProjectSnapshot`, `jsonqueries.QueryJsonDocuments`, `query.JSONContainsNullable`, `query.JSONKind`, `query.JSONNull`, `query.JSONObject`, `query.NullableRow`, `query.Param`, `query.Value` |
| C | `keyqueries/clause_parity_test.go` | `models.OrderFields`, `models.OrderFieldsAt`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields`, `models.UserFieldsAt`, `query.Alias`, `query.As`, `query.CTE`, `query.Correlate`, `query.CorrelatedValueQuery`, `query.Correlation`, `query.Count`, `query.ExistsQuery`, `query.InnerScope`, `query.OuterScope`, `query.Param`, `query.RowNumber`, `query.SelectCorrelatedValue`, `query.SelectValue`, `query.Set`, `query.Statement`, `query.Value`, `query.ValueQuery`, `query.When`, `query.Window`, `query.WindowFor` |
| C | `keyqueries/cursor_parity_test.go` | `keyqueries.BucketCountFieldsAt`, `keyqueries.BucketCountSelection`, `keyqueries.SelectBucketCount`, `models.QueryUsers`, `models.UserFields`, `query.Count`, `query.CursorFor`, `query.Param`, `query.SelectValue`, `query.Value`, `query.ValueCursorFor`, `query.When` |
| C | `lateralqueries/clause_parity_test.go` | `models.OrderFieldsAt`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFieldsAt`, `query.As`, `query.AsLateral`, `query.Correlate`, `query.CountField`, `query.CrossJoinLateral`, `query.Equal`, `query.ExistsQuery`, `query.InnerScope`, `query.OuterScope`, `query.Sum`, `reports.ProjectCorrelatedOrderSummary` |
| C | `models/orm_postgres_test.go` | `models.QueryUsers`, `models.UserAggregates`, `models.UserFields`, `models.UserRelations` |
| C | `models/orm_test.go` | `models.QueryCountries`, `models.QueryFriendships`, `models.QueryGroups`, `models.QueryLedgerEntries`, `models.QueryLocations`, `models.QueryMeasurements`, `models.QueryMemberships`, `models.QueryOrders`, `models.QueryUsers`, `models.QueryWriteRecords`, `models.UserRelations`, `query.Statement` |
| C | `projectionqueries/aggregate_contracts_test.go` | `models.OrderFields`, `models.QueryOrders`, `query.Count`, `query.Exists`, `query.HavingAnd`, `query.Sum`, `reports.OrderSummarySelection`, `reports.SelectOrderSummary` |
| C | `recursivequeries/sql_parity_test.go` | `models.QueryUsers`, `models.UserFields`, `models.UserFieldsAt`, `query.As`, `query.InnerJoin`, `query.LeftScope`, `query.On`, `query.RecordQuerySource`, `query.RecursiveCTE`, `query.RecursiveSelf`, `query.SelectRecord` |
| C | `reporting/orm_test.go` | `reporting.OperatorFields`, `reporting.QueryReportOperators` |
| C | `reporting/source_golden_test.go` | `query.As`, `query.Count`, `query.DefineProjection`, `query.InnerJoin`, `query.LeftScope`, `query.Map`, `query.NewProjectionField`, `query.On`, `query.Project`, `query.ProjectionColumn`, `query.ProjectionMapping`, `query.ProjectionQuery`, `query.ProjectionSource`, `query.RightScope`, `query.Sum`, `query.Trim`, `query.Value`, `reporting.MemberFields`, `reporting.MemberFieldsAt`, `reporting.OrderFields`, `reporting.OrderFieldsAt`, `reporting.OrderRelations`, `reporting.QueryReportMembers`, `reporting.QueryReportOrders` |
| C | `reports/rows_test.go` | `models.OrderFields`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields`, `query.Avg`, `query.Count`, `query.Statement`, `query.Sum`, `query.Value`, `reports.ProjectBuyerTotals`, `reports.ProjectUserSummary` |
| C | `scalarqueries/projection_sql_test.go` | `query.AddNullable`, `query.Alias`, `query.As`, `query.Avg`, `query.CTE`, `query.CountDistinctField`, `query.CountField`, `query.DecimalOf`, `query.Expression`, `query.Lower`, `query.Max`, `query.Min`, `query.Multiply`, `query.NullableRow`, `query.SelectValue`, `query.Sum`, `query.Trim`, `query.Value`, `scalarqueries.CalculationFieldsAt`, `scalarqueries.CalculationSelection`, `scalarqueries.QueryCalculationSamples`, `scalarqueries.SampleFields`, `scalarqueries.SelectCalculation` |
| C | `setqueries/derived_aggregate_sql_test.go` | `models.OrderFields`, `models.QueryOrders`, `models.QueryUsers`, `models.UserFields`, `query.Avg`, `query.Count`, `query.Set`, `query.Sum`, `query.Value`, `reports.BuyerTotalsFieldsAt`, `reports.ProjectBuyerTotals`, `reports.ProjectOrderSummary` |
| C | `temporalqueries/orm_test.go` | `query.Count`, `query.Max`, `query.Year`, `temporalqueries.ProjectCalendarSummary`, `temporalqueries.QueryTemporalSamples`, `temporalqueries.SampleFields` |
| C | `transactionqueries/orm_test.go` | `query.Statement`, `query.Value`, `transactionqueries.ProjectSummary`, `transactionqueries.QueryRecords`, `transactionqueries.RecordFields` |
| C | `windowqueries/orm_test.go` | `models.OrderFields`, `models.QueryOrders`, `query.CumeDist`, `query.DenseRank`, `query.NTile`, `query.PercentRank`, `query.Rank`, `query.RowNumber`, `query.Value`, `query.WindowFor`, `windowqueries.RankingRowSelection`, `windowqueries.SelectRankingRow` |

`reporting/orm_test.go:20` additionally compares `Operator.FoundryReference().Identity()` against `OperatorIdentity.Reference(id).Identity()`. Its generated identity/reference implementation must survive until that baseline is frozen, then be replaced through the native identity owner. `query.JSONKind`, `query.JSONNull` and `query.JSONObject` in the Snapshot test are shared compatibility aliases; their wire identity must remain unchanged even after generator cleanup.

Thus **34 model/projection directives (24 model, 10 projection)** and their generated outputs remain. On their removal, preserve User/Operator `FoundryIdentity` and reference semantics through the ORM identity owner; these methods currently still have their generated implementation. Remaining old compile-fail sources must migrate with the corresponding generated symbols and exact diagnostic registry entries, not be left failing for undefined symbols. `query.DefaultChunkSize` is a runtime compatibility alias and belongs to the later runtime retirement, not generator cleanup.

## Generation and verification

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r12 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

The barrier's targeted acceptance was run locally under decision 8. No verification is delegated to the lead.

| Command / scope | Result |
| --- | --- |
| `go build ./...` at framework root | PASS |
| `go vet ./...` at framework root | PASS |
| `go build` for the 17 owned packages with non-test source | PASS; the other 17 test-only packages compile in the test/race runs |
| `go vet`, `go test -timeout=20m`, `go test -race -timeout=20m` across all 34 owned packages | PASS |
| `go vet .` in consumer fixture root | PASS |
| `go test -timeout=20m -skip '^TestInvalidPublicContractsFailCompilation$' .` in consumer fixture root | PASS; includes footprint and module-pinned CLI checks |
| `go test -timeout=20m -v -run <owned compiler filter> .` | PASS: 373 intended compiler rejections, including all three migrated negatives |
| `go vet`, `go test`, `go test -race` for models/catalog after restoring the helpers | PASS |
| `python3 tools/goway_catalogue.py` | PASS after refreshing the three owned rule snapshots |
| `make generate` | PASS; 14 obsolete owned files removed, no new output generated |
| `make generate-check` | PASS; generated output current |
| `make docs-check` | PASS |
| `gofmt -w`, then `gofmt -l` on changed Go sources | PASS; no remaining output |
| `git diff --check` | PASS |

Initial failures were corrected: the first build named test-only packages explicitly; the catalogue required the three migrated source/diagnostic snapshots; root tests still referenced the two removed helpers, which are now restored exactly. Subsequent affected checks passed. The full commands, package roster, compiler filter, log hashes and source hashes are in the evidence JSON. Logs are under `/private/tmp/barrier-c-accept/`.

PostgreSQL execution remains opt-in and was not enabled in these runs; its test sources compiled in the ordinary and race builds. `make agent-smoke` is lane D's explicit barrier assignment. No integration or live-PostgreSQL result is claimed. All 28 C/A parity caller files listed above remain byte-identical to the base for lane A's freeze.

Shared files touched: `tests/fixtures/consumer/types_test.go` (two owned diagnostics), `blueprint/goway-data-layer/compilefail-mapping.tsv` (the three migrated rule snapshots); generator-owned manifests in lateralqueries, recursivequeries, reports, transactionqueries and windowqueries. No framework surface, footprint ceiling, module dependency or lead-owned blueprint/changelog was changed.

## Docs notes

The seven reports join/subquery results now use only handwritten rows schemas; README updated. The remaining barrier is oracle capture/replacement and coordinated root/negative-harness cutover, not a missing C runtime capability. Preserve byte-identical DTO/enum/contract output through final regeneration.
