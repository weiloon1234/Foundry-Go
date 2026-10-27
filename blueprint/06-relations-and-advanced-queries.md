# 06 — Relations and advanced queries

## Purpose and prerequisites

Prerequisite: [05](05-typed-model-and-query-core.md). Preserve model, scope,
cardinality, key and result typing through relationships and reporting. Stage 8
removes generated relation/projection namespaces; the contracts below now belong
to native declarations and shared compilation/execution owners.

## Boundaries and contracts

Declare slots and keys once in `orm.Define`; connect native relation descriptors
without repeating SQL names. Loaded slots distinguish not loaded, empty and
present. Direct, through, one-of-many and polymorphic relationships retain typed
keys, discriminators, target/pivot filters and cardinality checks. Complete target
and pivot hydration preserves link multiplicity. Invalid descriptors fail before
SQL, including descriptors adapted into another valid query.

`Query.With` adds eager relations. `LoadFrom` and `LoadMissingFrom` apply query-owned
depth/row budgets to existing parents without a parent statement. Parent rows close
before bounded child reads. A failed branch publishes no partial parents. Streaming
loads complete batches before delivery and resets relation budgets per batch.

| Family | Native owner and invariant |
| --- | --- |
| [Relations](../docs/guides/model-relations.md) | ORM descriptors and shared loader; concrete source/target/pivot types, bounded batching and loaded-slot policy. |
| [Aggregate slots](../docs/guides/model-aggregates.md) | Relation aggregate descriptors bind typed slots; grouped summaries preserve cardinality selections and exact codecs. `expr.RelatedValue` and `rows.WithValue` annotate complete models without surrogate slots, preserving retrieval callbacks. |
| [Projections](../docs/guides/model-projections.md) | `rows.Field[R,V]` and explicit mappings preserve value/nullability types. Scalar, record, set and complete-model reads share `CompileRead`, `PreflightRead`, `EachRead` and `Result`. DTO reads do not run model callbacks. |
| [Joins](../docs/guides/model-joins.md) | Typed alias/scope boundaries preserve filters and windows; outer sides widen nullability. ON expressions validate ownership and visibility, including nested/chained joins. |
| [Subqueries](../docs/guides/model-subqueries.md) | Scalar, membership and existence operands reuse shared SELECT lowering and bounds. Empty membership still validates its operand, and discards unused computed bindings. |
| [Correlations](../docs/guides/model-correlations.md) | Only enclosing scopes are visible. Nested grouping obligations, alias isolation and target requalification survive relation and aggregate composition. LATERAL uses explicit correlated scopes. |
| [Relationship filters](../docs/guides/model-relationship-filters.md) | Native predicates compile EXISTS without hydrating/multiplying parents. Construction errors and global scopes survive expression adaptation; outer-source shadowing remains rejected. |
| [CTEs](../docs/guides/model-ctes.md) | Dependencies compile once, before users; definitions are isolated, immutable and checked for shadowing/conflicts/cycles. Relation filters and aggregates participate in dependency discovery. |
| [Recursive CTEs](../docs/guides/model-recursive-ctes.md) | Typed complete anchor/step records and owned self-reference reuse shared set lowering. Construction bounds do not imply a runtime cycle limit. |
| [Sets](../docs/guides/model-set-operations.md) | Compatible record/scalar outputs share one set owner. Input ordering/windows stay inside operands; output order uses output scope. |
| [Distinct reads](../docs/guides/model-distinct.md) | Complete selection, binding-aware key matching and grouped validation preserve nullable/scalar/record results. Inner winner selection survives opposite outer ordering. |
| [Windows](../docs/guides/model-windows.md) | Partition/order, rank/navigation/aggregate windows and typed frames retain exact codecs. [RANGE and names](../docs/guides/window-ranges-and-names.md) share bounded dependency, inheritance and ownership validation. |
| [Aggregate filters](../docs/guides/model-aggregate-filters.md) | Filter predicates retain target/pivot qualification, CTE discovery and binding order without mutating reusable aggregates. Outer-only ordinary aggregate filters are rejected. |
| [Pagination](../docs/guides/model-pagination.md) | Numbered/simple/cursor reads keep result windows, explicit result ordering, null-aware keysets and typed tokens. COUNT removes irrelevant ordering but preserves ordering needed by LIMIT/OFFSET. |
| Writes and conflicts | Typed assignments/upsert/source-write mapping share native lifecycle and canonical SET ordering. Source CTE names cannot collide with internal names; scan errors retain identity and unmatched source counts remain validated. |
| Transactions | Record/row locks and transaction-only composition retain source requirements through every adapter. Wrong executor/ownership fails before SQL. |

## Acceptance and ownership

[Shared query compilation](../internal/querycore) owns SQL, canonical planning,
binding/comparison budgets and scope validation. [Native ORM](../database/orm) owns
declarations, lifecycle and hydration; [shared execution](../internal/ormcore) owns
bounded delivery, cancellation and callback isolation. No alternate query engine
or generated model decoder remains.

The [migration guide](../docs/guides/orm-migration.md) links compiled native examples.
The [transfer ledger](../docs/evidence/goway-stage8d3a-transfers.json) maps historical
assertions to executable native/shared tests. Independent fixtures retain SQL,
bindings, hydration, compiler negatives, footprint ceilings and frozen historical
observations. The [master](00-master-architecture-and-parity.md) retains dated
acceptance evidence; apply the [common gate](README.md#common-completion-gate).
