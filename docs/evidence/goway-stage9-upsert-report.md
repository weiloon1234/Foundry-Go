# Stage 9 Lane B: shared-model upsert cutover

Base: `fdaff938b71108081499a70d7bf2767f94a147c9`. No commits or git writes.

## Delivered

`upsertqueries` now consumes lane C's `models.Users`, `models.WriteRecords` and
`internal/queryfixture` declarations. Three PostgreSQL test files use typed ORM
assignments, `orm.Upsert`/`UpsertMany`, conflict policies and rows/expr composition.
No shared declaration was copied or edited.

Preserved assertions cover omitted/default/zero/null values, UUID/natural-key
identity, composite and named targets, partial/expression indexes under generic
plans, escaping independent of execution parameters, stored/proposed scopes,
trigger values, exact decimals, correlated CTE/scalar fallback and cardinality,
concurrent identity, atomic rollback and zero-transaction invalid/empty inputs.
`orm.Compare` plus `ChangeOf(...Key()).Assigned()` replaces the draft's ID getter
check and proves the caller's assignments still omit the synthesized key.
The filtered empty insert check uses filtered `UpsertMany(...DoNothing())`, which
exercises the shared insert validation before opening a transaction. ORM tables
cannot express a filtered `InsertMany` in the first place.

## Compiler cases and shared files

24 consumer cases migrated; their Go-way equivalents were already ported, so
catalogue disposition totals do not change:

- `conflict_calculated_target`, `conflict_calculation_model_scope`,
  `conflict_calculation_nullability`, `conflict_calculation_owner`,
  `conflict_calculation_selected`, `conflict_calculation_type`;
- `conflict_condition_owner`, `conflict_conversion`, `conflict_nonnullable`,
  `conflict_nullable_calculation_value`, `conflict_rows_ordinary_where`,
  `conflict_rows_select`, `conflict_rows_where_owner`, `conflict_target_owner`,
  `conflict_update_owner`, `conflict_value`;
- `index_key_owner`, `index_key_selected`, `index_key_string`,
  `index_predicate_owner`, `index_proposed_key`;
- `upsert_draft_owner`, `upsert_policy_owner`, `upsert_result_owner`.

Shared files touched: only those 24 rows in
`blueprint/goway-data-layer/compilefail-mapping.tsv` and corresponding diagnostics
in `tests/fixtures/consumer/types_test.go`. The other edits are 24 negative sources,
three upsert test sources, the package README and this slice's evidence.

No framework API, public method surface, persisted schema, wire contract,
generated file, manifest, OpenAPI or TypeScript changes. No footprint source or
budget changes; existing probes are not remeasured for this consumer-only edit.

## Remaining B roster

| Packages | Status |
| --- | --- |
| `upsertqueries` | Shared-model cutover complete in this delta; no A1 dependency remains. |
| `auditqueries`, `binarymodels`, `eventqueries`, `hookqueries`, `inputqueries`, `mutatorqueries`, `observerqueries`, `softqueries`, `tenantqueries`, `timequeries`, `vault` | Accepted migrations integrated in the base; no further A1-dependent caller found. |
| `linkqueries` | ORM callers complete; retained legacy creation-interface oracle and old/new SQL parity test remain intentionally. |
| `retrievalqueries` | ORM callers complete except the preserved complete-record streaming oracle; still awaits lane A's `rows.RecordRead` iteration integration. |
| `caching`, `coordination`, `messaging`, `limiting`, `redisdata`, `rediscommands`, `httpetags` | Existing consumers use typed stored values/inputs and explicit getters; no A1-dependent query/draft caller found. Redis remains opt-in. No edits in this delta. |

Generated compatibility declarations and callbacks remain for the lead's retirement
barrier. No dual observer registration is introduced.

## Acceptance

Results and exact command selection are recorded in
[the evidence JSON](goway-stage9-upsert.json). All Go commands use r8,
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`; PostgreSQL and Redis URLs/required modes
are explicitly disabled. PostgreSQL test sources compile; live execution belongs
to the lead. Initial vet/test compilation found one stale alias-scope method call;
it was replaced by the public `rows.Scope` function before the final runs.

## Docs notes

Mark the shared-model upsert dependency resolved. Use `orm.Upsert` with typed
assignments and `rows.ConflictRowsOf` for stored/proposed calculations. The legacy
`query.MaxInsertRows` reference is a compatibility constant only, not query-engine
usage. Keep retrieval's complete-record streaming item open until lane A lands it,
and keep generator retirement and live PostgreSQL acceptance separate.
