# Main H1 containment on the native ORM

Merge inputs: native branch `c4418838`, main `b8816902`, base `24a9b5cb`. The retirement boundary is unchanged: the deleted query engine and generated persistence adapters stay deleted. No retirement exemptions were added.

## Resolution and shared owners

- Initial conflicts: 126 deleted/updated, 78 updated/updated, 13 added/added and 8 false rename artifacts (updated/added).
- Framework faults capture immutable diagnostics through `internal/safeerror`; native query composition and cleanup retain them through `safeerror.Join`. Application error inspection stays inside `internal/errorgraph` and `internal/callback`.
- Native mutators and unhooked retrieval/autocommit hydration now use isolated callbacks. Native reconciliation errors and cursor input errors capture trusted text at construction without exposing candidates or causes.
- The connector-owned `internal/sqlcallback` boundary replaces the older shallow database argument adapter. Driver-facing Valuer/Scanner errors are sealed before database/sql can format them.
- Canonical UPDATE SET rendering, validated capability families and the native declaration roles remain in place.

## Deleted-file mapping

Old locations below refer to main at `b8816902`, not live paths. Each row covers all H1 hunks in that deleted file; line ranges include import-only changes. Native locations are the shared replacement owners, not copied generic engine code. Generated CRUD checks are now the shared write/change/adjustment owners; there is no generated persistence implementation to retain.

| Deleted main file and H1 hunk locations | Native owner locations | Disposition |
| --- | --- | --- |
| `countries/country_foundry.gen.go:14-20, 830-836, 1379-1385, 1394-1400, 2318-2427, 2663-2669, 2722-2728, 2922-2928, 3062-3075, 3344-3350` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `database/query/alias.go:4-10, 35-41, 48-54` | `internal/querycore/alias_core.go:13` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/chunk.go:7-13, 44-50, 53-59, 85-91, 93-103, 148-154, 158-164, 171-177, 187-193` | `database/orm/chunk.go:29` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/comparison.go:5-11, 17-23` | `internal/querycore/compiler.go:95` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/compile_insert.go:3-9, 38-44, 50-56, 71-77` | `internal/querycore/mutation_core.go:13` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/compile_insert_select.go:7-13, 20-26, 35-47, 54-60, 71-77` | `internal/querycore/compile_insert_select.go:11` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/compile_source_write.go:3-9, 16-36, 41-58` | `internal/querycore/compile_source_write.go:14` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/compiler.go:5-11, 70-76` | `database/orm/query.go:110` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/conditional_expression.go:6-12, 33-39, 68-74, 148-154` | `database/orm/expr/conditional.go:22` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/conflict_mutator.go:6-12, 23-29, 33-39` | `internal/querycore/conflict_mutator.go:25` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/conflict_rows.go:3-9, 53-59` | `internal/ormcore/conflict.go:29` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/correlated_value.go:3-9, 71-77, 81-87` | `database/orm/rows/correlated_scalar.go:20` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/correlation_binding.go:4-10, 17-23, 32-38` | `internal/ormcore/correlation.go:29` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/correlation_scope.go:3-9, 29-35` | `internal/querycore/correlation_scope.go:1` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/cte.go:4-10, 34-40, 64-70` | `internal/ormcore/record_composition.go:19` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/cursor_boundary.go:7-13, 28-34` | `internal/querycore/cursor_boundary.go:28` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/cursor_pagination.go:7-13, 42-48, 54-64, 166-172` | `database/orm/cursor.go:93` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/cursor_token.go:12-18, 51-59, 138-144` | `internal/querycore/cursor_token.go:31` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/cursor_validation.go:2-8, 20-29, 49-55` | `internal/querycore/cursor_error.go:19` | Owned cursor diagnostic and shared cursor contract; hostile cause identity/formatting regression ported. |
| `database/query/definition.go:9-15, 51-67` | `database/orm/declaration.go:142` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/encrypted_fields.go:11-17, 66-72, 98-104, 109-115, 117-123, 144-150, 199-205, 225-231, 242-248, 308-312` | `internal/querycore/encrypted_fields.go:32` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/error_graph_test.go:88-95` | `database/orm/outcome_graph_test.go:16` | Native outcome graph regression retains bounded custom-As inspection and reconciliation; wrapper matched with errors.As. |
| `database/query/execute_autocommit.go:2-13, 36-67, 80-86, 89-93` | `database/orm/write.go:634` | Isolated hydration, safe cleanup joining and no partial publication; hostile decoder regressions retained in database tests. |
| `database/query/execute_insert.go:8-14, 94-100` | `database/orm/conflict_write.go:18` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/execute_mutation.go:2-7, 9-15, 32-42, 73-79, 84-90, 182-198, 220-226, 228-234, 243-249` | `database/orm/write.go:727` | Owned typed reconciliation candidate/outcome diagnostic; native write paths share writeFailure. |
| `database/query/execution.go:5-17, 36-42, 107-113` | `database/orm/query.go:110` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/explain.go:8-14, 147-153` | `internal/querycore/explain.go:124` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/field_metadata.go:6-12, 48-54, 99-105, 107-113` | `internal/querycore/record_equality.go:21` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/field_mutator.go:2-13, 44-59, 116-122` | `internal/querycore/field_mutator.go:33` | Isolate typed mutator callback before retaining output; panic/Goexit and hostile formatting regressions ported. |
| `database/query/insert_select.go:7-13, 31-37, 72-78` | `database/orm/source_write.go:167` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/join.go:4-10, 113-119` | `internal/ormcore/join.go:15` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/joined_hydration.go:8-14, 30-36, 38-44` | `internal/ormcore/relation_row.go:32` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/json_expression.go:3-9, 24-30` | `database/json_kind.go:17` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/json_path.go:7-13, 81-87, 89-95, 104-110` | `internal/querycore/json_path.go:16` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/json_path_predicates.go:3-9, 26-32` | `database/orm/expr/json_path.go:56` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/lateral_join.go:2-8, 18-24` | `database/orm/rows/lateral.go:51` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/locked_model.go:5-11, 89-95, 140-146, 152-158` | `database/orm/query.go:110` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/locked_result.go:6-12, 55-61, 104-110` | `internal/ormcore/record_lock.go:22` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/locking.go:6-12, 55-61` | `database/orm/query.go:110` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/lookup.go:6-12, 24-30, 37-50, 68-74, 99-105` | `database/orm/lookup.go:44` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_conventions.go:6-12, 63-69` | `database/orm/declaration.go:142` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_hooks.go:6-12, 79-85, 99-105` | `database/observer_owner.go:12` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_identity.go:8-14, 32-38, 45-51, 77-83, 102-108` | `database/orm/identity.go:35` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_timestamps.go:11-17, 31-37, 46-55, 69-75` | `database/orm/write.go:85` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_value_mapping.go:4-10, 29-41` | `internal/querycore/sql_mapping.go:8` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/model_write_key.go:2-15, 17-30` | `database/orm/write_keyed.go:15` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/mutation_input.go:5-11, 18-24` | `internal/querycore/field_mutator.go:37` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/mutation_validation.go:5-11, 20-26` | `internal/querycore/mutation_validation.go:24` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/mutation_values.go:2-8, 37-43` | `database/orm/write_draft.go:34` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/pagination.go:6-12, 37-43, 62-68, 74-80, 87-93, 139-145` | `database/orm/pagination.go:34` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/projection.go:6-12, 23-29, 32-38, 57-63, 81-87` | `database/orm/rows/projection.go:45` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/projection_definition.go:6-12, 58-64` | `database/orm/rows/declaration.go:84` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/projection_reorder.go:2-8, 12-18` | `internal/querycore/report.go:12` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/prune_batch.go:5-11, 24-30, 35-41` | `database/orm/prune.go:18` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/read_lifecycle.go:3-15, 61-67, 132-138, 149-159` | `internal/ormcore/retrieval.go:23` | Isolated hydration, safe cleanup joining and no partial publication; hostile decoder regressions retained in database tests. |
| `database/query/read_lifecycle_chunks.go:8-14, 19-25, 57-63, 72-78, 83-89` | `internal/ormcore/record_iteration.go:22` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/read_result.go:6-12, 31-37, 77-83` | `database/orm/rows/read_terminal.go:26` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/record_source.go:3-9, 28-34` | `internal/ormcore/record.go:46` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/recursive_cte.go:4-10, 25-31, 51-57, 60-66` | `internal/ormcore/record_composition.go:19` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_aggregate.go:2-12, 31-40, 52-64, 68-74, 81-87` | `internal/querycore/relation_aggregate.go:39` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_aggregate_loading.go:7-13, 42-48, 118-144` | `database/orm/relation_aggregate_loading.go:100` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_aggregate_source.go:2-8, 27-33, 41-47, 55-61` | `database/orm/relation_aggregate.go:49` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_direct.go:6-12, 36-42, 46-52, 59-68, 77-89, 204-210, 214-220, 240-246, 353-359, 370-376` | `database/orm/relation_loading.go:100` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_extension.go:5-11, 49-59, 74-87, 112-134` | `database/orm/relation_tree.go:74` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_loading.go:7-13, 32-38, 45-51, 58-68, 108-114, 116-122, 143-149` | `internal/querycore/relation_loading.go:17` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_lookup.go:7-13, 40-46, 50-56, 74-80` | `database/orm/lookup.go:44` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_morph.go:6-12, 26-32, 49-55, 70-76, 123-129, 158-164` | `database/orm/relation_morph.go:16` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_of_many.go:2-8, 55-64` | `database/orm/relation_join_plan.go:34` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_pivot_sync.go:9-15, 82-88, 108-117, 172-178, 233-246, 260-266, 279-285, 294-300, 312-318, 329-335, 352-358, 373-379, 443-449, 493-499, 514-520` | `database/orm/relation_write_core.go:44` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_through.go:7-13, 54-60, 110-116, 123-135, 139-149, 165-171` | `database/orm/relation_through.go:52` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_through_hop.go:7-13, 53-65, 168-174, 189-195, 208-214` | `database/orm/relation_join_plan.go:34` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_through_loading.go:6-12, 26-32, 55-61, 68-88` | `database/orm/relation_through_loading.go:50` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_value.go:6-12, 21-27, 42-48, 104-110` | `database/orm/relation_aggregate_loading.go:100` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_write.go:5-11, 43-52, 64-70, 91-97, 121-127` | `database/orm/relation_write.go:35` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/relation_write_keys.go:6-12, 53-70` | `database/orm/relation_write_core.go:44` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/result_cursor.go:7-13, 32-38, 43-49, 81-93, 104-110, 119-125, 129-135, 137-143, 146-160, 163-172` | `internal/querycore/result_cursor.go:13` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/result_pagination.go:5-11, 15-21` | `database/orm/rows/result_read.go:252` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/row_expression.go:3-9, 74-80` | `database/orm/expr/row.go:23` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/select_record.go:3-9, 29-35` | `internal/ormcore/record.go:46` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/set.go:5-11, 37-43, 61-76, 98-104, 118-124` | `internal/querycore/record_layout.go:20` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/set_write.go:7-13, 62-68, 86-92, 117-123` | `internal/querycore/set_write.go:1` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/soft_delete_scope.go:8-14, 42-51` | `internal/querycore/soft_delete_scope.go:24` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/source_write.go:7-13, 33-39` | `database/orm/source_write.go:167` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/source_write_execution.go:7-13, 37-43, 82-93, 125-131` | `database/orm/source_write_execution.go:26` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/subquery.go:4-10, 60-66, 83-89, 104-117` | `internal/querycore/subquery.go:38` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/temporal_units.go:5-11, 86-92, 113-119, 121-134` | `internal/querycore/scalar_arguments.go:36` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_alias.go:2-8, 17-23` | `database/orm/rows/transaction_record.go:90` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_correlated_lock.go:2-15` | `database/orm/rows/transaction_correlated_lock.go:8` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_correlated_value.go:2-8, 60-66` | `database/orm/rows/transaction_correlated_scalar.go:21` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_correlation.go:2-8, 24-30` | `database/orm/rows/transaction_correlation.go:38` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_cte.go:2-8, 15-21` | `database/orm/rows/transaction_record.go:69` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_join.go:2-8, 38-44` | `database/orm/rows/transaction_join.go:14` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_lateral.go:2-8, 17-23, 51-57` | `database/orm/rows/transaction_lateral.go:52` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_source.go:2-8, 33-39, 50-56, 69-75` | `internal/ormcore/record_lock.go:22` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_subquery.go:2-8, 16-22, 57-63, 103-109` | `database/orm/rows/transaction_subquery.go:30` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/transaction_value.go:5-11, 33-39` | `database/orm/rows/transaction_subquery.go:30` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/unique_lookup.go:6-12, 24-30, 35-48, 63-69` | `database/orm/lookup.go:44` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/window.go:5-11, 57-63, 86-92, 131-137` | `internal/querycore/window.go:1` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/window_function.go:4-10, 88-94` | `database/orm/expr/window.go:23` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/window_named.go:3-9, 26-32` | `internal/ormcore/window.go:22` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/window_range.go:6-12, 53-59, 74-80, 101-107, 119-125` | `internal/querycore/window_range.go:21` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/write_each.go:6-12, 19-25, 70-76, 114-120, 164-170, 175-181, 197-210` | `database/orm/write_each.go:27` | Immutable validation diagnostics at the native/shared owner. |
| `database/query/write_lookup.go:3-15, 29-35, 40-46, 96-102, 127-140, 174-180` | `database/orm/write_lookup.go:21` | Immutable validation diagnostics at the native/shared owner. |
| `internal/attachmentstore/file_foundry.gen.go:14-20, 613-619, 1159-1165, 1174-1180, 1923-2032, 2268-2274, 2327-2333, 2527-2533, 2667-2680, 2949-2955` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/attachmentstore/variant_foundry.gen.go:13-19, 504-510, 957-963, 972-978, 1550-1627, 1823-1829, 1882-1888, 2050-2056, 2174-2187, 2384-2390` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/auditstore/entry_foundry.gen.go:15-21, 561-567, 1008-1014, 1023-1029, 1608-1681, 1872-1878, 1931-1937, 2095-2101, 2217-2230, 2416-2422` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/challengestore/entry_foundry.gen.go:13-19, 396-402, 723-729, 738-744, 1019-1052, 1193-1199, 1252-1258, 1376-1382, 1478-1491, 1587-1593` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/challengestore/subject_foundry.gen.go:13-19, 389-395, 671-677, 686-692, 850-867, 988-994, 1047-1053, 1155-1161, 1249-1262, 1322-1328` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/extensionstore/meta_foundry.gen.go:14-20, 471-477, 825-831, 840-846, 1188-1233, 1389-1395, 1448-1454, 1584-1590, 1692-1705, 1830-1836` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/extensionstore/setting_foundry.gen.go:14-20, 518-524, 911-917, 926-932, 1391-1448, 1619-1625, 1678-1684, 1826-1832, 1940-1953, 2105-2111` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/extensionstore/translation_foundry.gen.go:13-19, 474-480, 831-837, 846-852, 1227-1272, 1428-1434, 1487-1493, 1623-1629, 1731-1744, 1869-1875` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/idempotencystore/record_foundry.gen.go:13-19, 446-452, 835-841, 850-856, 1294-1347, 1513-1519, 1572-1578, 1716-1722, 1828-1841, 1983-1989` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/jobarchive/failure_foundry.gen.go:13-19, 447-453, 837-843, 852-858, 1281-1334, 1500-1506, 1559-1565, 1703-1709, 1815-1828, 1969-1975` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/mfastore/factor_foundry.gen.go:13-19, 501-507, 864-870, 879-885, 1265-1310, 1466-1472, 1525-1531, 1661-1667, 1769-1782, 1905-1911` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/notificationstore/delivery_foundry.gen.go:14-20, 450-456, 804-810, 819-825, 1153-1194, 1345-1351, 1404-1410, 1536-1542, 1642-1655, 1769-1775` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/notificationstore/envelope_foundry.gen.go:15-21, 519-525, 897-903, 912-918, 1314-1363, 1524-1530, 1583-1589, 1723-1729, 1833-1846, 1978-1984` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/notificationstore/inbox_foundry.gen.go:14-20, 431-437, 770-776, 785-791, 1095-1132, 1278-1284, 1337-1343, 1465-1471, 1569-1582, 1687-1693` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/outboxstore/message_foundry.gen.go:15-21, 508-514, 910-916, 925-931, 1395-1452, 1623-1629, 1682-1688, 1830-1836, 1944-1957, 2107-2113` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/sessionstore/entry_foundry.gen.go:13-19, 513-519, 972-978, 987-993, 1658-1735, 1931-1937, 1990-1996, 2158-2164, 2282-2295, 2490-2496` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/sessionstore/subject_foundry.gen.go:13-19, 389-395, 671-677, 686-692, 850-867, 988-994, 1047-1053, 1155-1161, 1249-1262, 1322-1328` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/tokenstore/entry_foundry.gen.go:13-19, 423-429, 786-792, 801-807, 1184-1229, 1385-1391, 1444-1450, 1580-1586, 1688-1701, 1824-1830` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/tokenstore/family_foundry.gen.go:14-20, 528-534, 951-957, 966-972, 1491-1556, 1737-1743, 1796-1802, 1952-1958, 2070-2083, 2251-2257` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/tokenstore/subject_foundry.gen.go:13-19, 389-395, 671-677, 686-692, 850-867, 988-994, 1047-1053, 1155-1161, 1249-1262, 1322-1328` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/webhookstore/delivery_foundry.gen.go:13-19, 446-452, 815-821, 830-836, 1240-1289, 1450-1456, 1509-1515, 1649-1655, 1759-1772, 1906-1912` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/webhookstore/endpoint_foundry.gen.go:13-19, 451-457, 760-766, 775-781, 1037-1066, 1202-1208, 1261-1267, 1381-1387, 1481-1494, 1583-1589` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |
| `internal/webhookstore/secret_foundry.gen.go:13-19, 381-387, 684-690, 699-705, 938-963, 1094-1100, 1153-1159, 1269-1275, 1367-1380, 1458-1464` | `database/orm/write.go:85`; `database/orm/changes.go:86`; `database/orm/adjustment.go:15` | Generated CRUD retired; shared native insert bounds, changes and adjustment validation capture diagnostics once. |

## False rename artifacts and regression ports

| Main assertion | Native coverage |
| --- | --- |
| Binding error retention | `database/orm/rows/binding_error_transfer_test.go` (existing transfer) |
| Cursor codec classification/redaction | `internal/querycore/cursor_boundary_transfer_test.go` (existing transfer) |
| Concrete observer ownership | `database/orm/observer_capability_test.go`, `database/observer_owner_test.go` (all native concrete forms) |
| Scope callback and Explain hostile errors | `database/orm/callback_containment_test.go` |
| Lazy source failure, captured/later join, subquery, aggregate and relation paths | `database/orm/rows/scope_source_containment_test.go`, `database/orm/scope_source_failure_test.go` |
| Typed immutable diagnostics and reconciliation | `database/orm/typed_diagnostics_test.go` |
| Conflict provenance / scoped competitor visibility | `database/orm/insert_lookup_postgres_test.go` |
| Private databaseFailure helper | `internal/querycore/diagnostics.go` captures trusted native operations; public `database.NewError` remains untrusted. |

The retired enclosing-transaction wrapper has no native equivalent: native callbacks receive the actual transaction. Observer regression coverage checks actual transactions, nested transactions, pools, primary executor values/pointers and sessions.

Main-side metadata decoder matching now tests the native shared extension-slot loading owner directly (`metadata/slot_error_graph_postgres_test.go`); no retired slot facade is restored. Main diagnostics tests in extensions, pagination and auth use native identity/page constructors.

## B39 reconciliation

Keep native token/session/challenge store declarations and the two-index first-subject insert behavior. The shared credential classifier retains bounded custom-As outcome recognition and fail-closed precedence. Main adds immutable diagnostic wrappers and containment; both the concurrency/contention and classifier regression families remain. The insert owners are `auth/token/postgres/backend.go:129`, `auth/session/postgres/backend.go:118` and `auth/challenge/postgres/subject.go:33`; `internal/credential/failure.go:14` is the single bounded classifier.

## Integration-specific findings

- The native primitive adapter initially entered H1 callback isolation twice (25 allocations versus the direct codec baseline of 13). Its representation dispatch now lives in `database/codec/primitive.go`, accessed with a validated metadata capability; the returned codec owns one isolation boundary. The original allocation-parity test passes unchanged.
- The aggregate-family footprint initially measured 4,385 functions against its unchanged 4,369 ceiling. Removing the redundant representation shell and sharing codec decode/scan isolation brings it to 4,363 functions (19,421,310 archive bytes). Decode and Scan keep their distinct diagnostic operations and publish only successful results.
- Race instrumentation made exact compiler allocation samples alternate between 72 and 73 allocations on either side. Exact allocation guards remain unchanged in non-race runs, matching the existing primitive guard; scope reuse and SQL/binding equivalence remain race-covered in `internal/querycore/statement_shape_scope_test.go`.
- Main collection-isolation tests exposed the branch's internal slice-adoption shortcut. `FoundryCollection` and `FoundryLinked` retain validated seals and now copy caller inputs; both sides' assertions remain.
- `internal/querycore.DatabaseFailure` captures native operation diagnostics. Public `database.NewError` still cannot grant trusted diagnostic provenance.
- H1's construction guard also covers branch-only model metadata and tooling. Those sites use safe owners; neither the H1 guard nor retirement guard gained exemptions.
- Generator publication updated the two framework enum outputs whose ownership manifests required refresh. The branch version of the countries enum was selected intact before generation. No generated file was manually merged.

## JSON map-key compatibility correction

The lead selected the merge-base `encoding/json` persisted-bytes contract. Main H1's named-string key method dispatch at `internal/jsoncodec/methods.go:126` maps to the shared owner's early string-kind check: string keys are written directly, before text methods. Non-string keys retain isolated AppendText-before-MarshalText dispatch, pointer/nil-pointer handling and native integer conversion. Error redaction and panic/Goexit classification are unchanged.

`value/json_key_encoding_test.go` restores `TestJSONKeyEncodingRetainsStringKeyPrecedence`, including a panicking method that must never run. A differential test compares transport output with `encoding/json` for strings, named strings with text methods, pointers, nil pointers, signed/unsigned integers, method-bearing integers and struct keys. The differential oracle runs the same installed Go compiler with `GOEXPERIMENT=nojsonv2`, because the experiment-enabled `encoding/json` delegates to v2 and reproduces the H1 precedence regression. Its child uses only the standard library, a two-minute deadline and bounded output. The shared owner’s method-selection test now uses the correct string-key oracle; non-string method and hostile-error cases remain. `contract/json_key_test.go` checks named-string key encoding and typed decoding together. Existing value, contract and HTTP panic/Goexit/error round-trip assertions now use comparable non-string keys: those callbacks must run, while string-key callbacks must not. Their classification, redaction and exact HTTP response assertions are retained.

Consumer audit:

| Consumer | Shared path / disposition |
| --- | --- |
| Persisted `value.JSON` and transport JSON | `value/json.go:40` → Marshal; `value/json_encoding.go:60` → MarshalWrite; both use the corrected hook |
| Typed map-key canonicalization and ORM JSON paths | `value/json_map.go:85` → EncodeJSON; `internal/querycore/json_path.go:23` → NewJSON; no separate encoding precedence |
| HTTP contract JSON | `http/endpoint_response.go:56` → `contract/json_encoding.go:51` → EncodeJSON; streaming events and status/credential responses use the same contract encoder |
| Datatable rows and requests | `datatable/manager.go:397` and `datatable/http.go:32` use contract Encode; protected publication artifacts are checked without regeneration |
| Lists and optional values | `value/list.go:23`, `value/optional.go:89` use Marshal; list streaming delegates to the same hook with the caller’s encoder options |
| Cache, config tables and pgx JSON | `cache/codec.go:52`, `config/table.go:82`, `database/postgres/callback_codec.go:355` use Marshal with default encoding/json-compatible options |
| Enum wire scalars | `enum/descriptor.go:95` encodes values rather than map keys; value-method behavior is unchanged |
| Config and contract decoding | Unmarshal-only consumers; decoding precedence is unchanged |

The CHANGELOG records this as an H1 regression correction. No generated or protected contract bytes are edited.

## Acceptance

Executed on the resolved working tree with `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r20`, `GOMAXPROCS=4`, `GOFLAGS=-p=4`, and `GOWORK=off`. Root tests, conflicted-package races and fixtures sourced the existing private test configuration with `FOUNDRY_TEST_POSTGRES_REQUIRED=1`.

| Check | Final result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| Root `make test` | PASS: 255 packages, including retirement and H1 construction guards |
| Conflicted-package races | PASS: 53 root packages plus consumer `hookqueries`; PostgreSQL required |
| `make fixture-check` | PASS: 149 packages; frozen replay, compile-fail cases, footprint ceilings and PostgreSQL smoke cases |
| `make generate-check` | PASS |
| `make docs-check` | PASS |
| `FOUNDRY_TEST_GOPLS=/Users/weiloon/Projects/rust-go/Foundry-Go/bin/gopls make agent-smoke` | PASS: 506 tests/subtests, 308 seconds |
| `make fmt-check` | PASS |
| `git diff --check` | PASS |
| `git diff --name-only --diff-filter=U` | Empty |

The three changed fixture-test census hashes were refreshed (articles writes, hook callback errors and HTTP assets), preserving sorted paths and existing entry format. Frozen bundles are untouched. Saved merge stages and command logs are outside the live tree at `/private/tmp/foundry-goway-merge-h1-evidence`; the retirement guard correctly rejects historical source copies inside the repository.

The first runs exposed the integration findings above; final root, race and fixture gates were rerun after the shared codec correction. The initial gate described main-side JSON behavior. The lead-approved JSON compatibility correction was then verified separately on r20: `go test -race ./internal/jsoncodec ./value ./http ./contract` (value rerun after adapting its non-string failure fixture), root `make test` with PostgreSQL required, `make datatable-contracts`, `make generate-check`, `make docs-check` and `make fmt-check` all passed. The protected datatable, reporting and cursor contract bytes are unchanged. No fixture test changed in this follow-up, so no additional census digest changed. Logs use the `json-` prefix in the evidence directory above. No commit, merge abort or reset was performed.
