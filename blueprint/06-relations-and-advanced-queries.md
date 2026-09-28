# 06 — Relations and advanced queries

## Purpose and prerequisites

Prerequisite: [05](05-typed-model-and-query-core.md). Preserve typing through relationships and reporting instead of falling back to string columns and generic maps.

Rust references: `src/database/relation.rs`, `query.rs`, `projection.rs`, `aggregate.rs`, `collection_ext.rs`, `ast.rs`, `compiler.rs`; `tests/database_acceptance.rs`; `docs/guides/database.md`. Inspect Starter's self-referencing user/introducer relation and datatable projections as usage examples.

## Boundaries and contracts

Relations describe source model, target model, cardinality, and compatible key types. Declare keys through generated typed field descriptors; never repeat the SQL column name or database type to define a relation. Eager-loaded relation values distinguish not loaded, loaded empty, and loaded with a value.

Delivered direct-relation consumer shape:

```go
users, err := models.QueryUsers().
    With(models.UserRelations().Introducer).
    All(ctx, db)
```

Generated relation descriptors support belongs-to, has-one, has-many and many-to-many, including self-relations and custom/natural keys. Polymorphic framework extensions use registered typed owner descriptors; arbitrary user-defined polymorphic joins remain an explicit feature, not an untyped shortcut.

Current slices: [typed relations](../docs/guides/model-relations.md) provide belongs-to, has-one, has-many and many-to-many. Model fields declare `relation.One[T]`/`Many[T]`/`Through[T, P]`; a handwritten `DefineRelations` method connects generated key descriptors in ordinary Go. Generation owns relation-set typing, source/target/pivot metadata and typed slot attachment. This preserves key completion/type checking without duplicating SQL column strings. [Relation aggregates](../docs/guides/model-aggregates.md) add computed `relation.Value[V]` slots. The advanced capabilities below share this typed metadata; the master records their verification status.

Many-to-many loads each source-key batch with an aliased inner join through the shared SELECT AST/compiler. Complete target and pivot decoders produce typed `Link[T, P]` values. Target/pivot filters, combined SQL ordering, nested branches, natural keys and self-relations share existing loaded states and budgets. Distinct pivot rows remain separate links; a pivot matching multiple targets fails cardinality checks. Separate declared projections can join pivot models through the typed join API. Lifecycle-aware attach/detach helpers depend on milestone 07's write pipeline.

Direct eager loading batches distinct keys, closes parent rows before child queries, supports scoped/nested loads and exposes `Load`/`LoadMissing` for existing model slices. Loaded states remain explicit. Shared limits cap nesting, fetched rows and attached model expansion. Singular cardinality violations and later-branch failures discard all returned parents. `Each` with eager clauses now loads complete bounded batches before invoking callbacks, with relation budgets reset per batch.

Aggregate declarations use `DefineAggregates() ItsAggregateSet`, with `query.Related(typedRelation, typedAggregate)` pairing input ownership and result type. Ordinary `With` loads generated aggregate slots. Grouped SQL supports row/field/distinct counts, existence, nullable extrema, exact integer/decimal sums and averages, and approximate float summaries. Many-to-many targets or typed pivot inputs retain their scopes and cardinality checks. Each relation aggregate executes independently per key batch. Scalar/grouped projections also support typed HAVING predicates and aggregate ordering, with explicit grouped-column validation before execution.

Delivered [projection results](../docs/guides/model-projections.md) use `//foundry:projection` on ordinary Go structs. Generated `ResultSelection[Input]` fields and `SelectResult` functions check source ownership, destination types and nullability. Field values and aggregate expressions share the SELECT AST, with complete result decoders and pre-execution mapping/grouping validation. Queries preserve typed input predicates, field ordering, grouping and limit/offset; result-window Count/Exists and streaming are available. Projection queries do not expose model mutations or eager-loading methods.

[Typed joins and aliases](../docs/guides/model-joins.md) now expose inner, left, right, full and Cartesian joins. `As[AliasTag]` gives each model occurrence a distinct type; generated scoped field sets preserve its model values and operator capabilities. `On` checks key compatibility and left/right ownership, with composite/alternative conditions and per-side ON predicates. Field scopes must be brought into the resulting join before projection. Nullable scope helpers are required for missing outer sides, and join kind participates in type identity. Chained joins preserve earlier nullable scopes.

Generated `ProjectResult(source)` builders infer the joined scope and provide typed field selection methods before returning the existing projection runtime. Filtered/ordered/windowed model inputs remain derived sources with their pre-join semantics. The shared compiler validates isolated nested scopes, aliases, grouped fields and parameter/expression bounds before SQL. Runtime declaration checks reject repeated SQL names and duplicate typed alias identities.

`SelectRecord(source, scope)` selects all persisted fields of a model or all declared fields of a projection from a preserved scope. It reuses the generated decoder and the existing read-only projection runtime, preserving join multiplicity, concrete result typing, optional/required first results and ordinary Go slices. Model/alias/set scopes carry complete metadata through non-nullable join adapters. Missing outer sides require declared nullable projections; whole-record selection never constructs partial models. Recursive model CTE steps use this same complete joined-record boundary.

[Derived records and subqueries](../docs/guides/model-subqueries.md) extend `As` to complete projection queries. Generated record field sets share model codec/operator emission and preserve nullability through outer joins. A declared joined result can become a new right-side source. `SelectValue` reuses projection validation/execution while retaining its input scope and single output type. Typed IN, nullable IN, EXISTS and scalar subqueries compile through the shared SELECT AST, with isolated scopes and shared bounds/parameters. Scalar empty/multiple-row semantics are explicit; nullable inputs retain one nullable layer.

[Explicit correlations](../docs/guides/model-correlations.md) retain `Correlation[Outer, Inner]` in their field/predicate types. `Correlate` plus generated scoped field sets supports typed column comparisons, correlated EXISTS/membership/scalars, nested parent/grandparent references and nullable records on either side. Correlated value queries cannot execute independently or enter ordinary uncorrelated query interfaces. The shared compiler carries enclosing grouping requirements, isolates derived sources, validates ON visibility and requalifies captured outer references for many-to-many loading.

[Relationship existence filters](../docs/guides/model-relationship-filters.md) expose generated `WhereHas`/`WhereDoesntHave` methods and parent-owned `Exists()` predicates on direct and many-to-many descriptors. Target/pivot filters can nest further relationship predicates; bounded alias analysis avoids capture across self-relations and explicit subqueries. Existence does not hydrate related rows, multiply parent rows or apply eager/order clauses. A descriptor remains reusable with `With`, and generated filters preserve concrete model lookup/write signatures.

[Typed nonrecursive CTEs](../docs/guides/model-ctes.md) carry complete model/projection record types and their definitions through existing aliases, joins and subqueries. Reused descriptors emit once, dependencies compile first, and explicit materialization options preserve immutable composition. CTE predicates work in scoped model writes and direct/through/aggregate relationship loading. One bounded AST traversal owns dependency discovery and relation alias analysis; conflicting definitions, physical-table shadowing and cycles fail before execution. Definitions remain independent of surrounding row scopes.

[Typed set operations](../docs/guides/model-set-operations.md) combine complete record queries from compatible result types and single-value queries from compatible value types. Union/intersection/difference and their duplicate-preserving forms use one set node in the shared AST. Combined results expose a separate `Set[Record]` scope for generated fields; each input retains its filters and window. Projection/set execution shares complete decoders and collection/streaming behavior. Sets compose with CTEs, joins, projections, typed subqueries, correlations and model-write predicates.

[Typed recursive CTEs](../docs/guides/model-recursive-ctes.md) use a complete anchor record and a synchronous construction callback receiving `RecursiveSelf[Record]`. The callback returns the same model/projection record type through the existing source boundary. `RecursiveCTE` uses UNION, while `RecursiveAllCTE` retains duplicate paths. The shared set compiler emits the recursive top-level UNION directly; the bounded CTE planner validates owned single self-reference, dependencies and PostgreSQL placement restrictions. Aliases, natural keys, report fields, result decoders and query cancellation use existing contracts. Construction bounds do not imply a runtime recursion limit or general cycle detection.

[Typed distinct reads](../docs/guides/model-distinct.md) use the complete selection pipeline for row deduplication and PostgreSQL `DistinctOn` field keys. Model selections are read-only and keep complete generated decoders; projected/scalar/correlated result types remain intact. Parameter-aware ordering reuses selected expressions, counting preserves deduplicated rows, and source composition shares the existing AST. Distinct keys reuse scope-owned `Group` descriptors rather than strings.

[Typed window expressions](../docs/guides/model-windows.md) add scope-inferred partition/order definitions, rank/distribution/navigation functions, aggregate `Over`, ROWS/GROUPS frames, current/unbounded RANGE positions and exclusions. Results retain generated projection types, exact codecs and nullable wrappers. Windows share SELECT validation, dependency discovery, correlation qualification, grouping analysis and result execution. [Typed RANGE distances and named windows](../docs/guides/window-ranges-and-names.md) extend that implementation with exact numeric distances, calendar/elapsed intervals, nullable ordering and SELECT-local definitions. Window descriptors carry their definitions; reuse emits each name once, dependencies first. Inheritance, duplicate names, scope ownership and invalid distances are checked before execution. The master records verification status for this extension.

[Aggregate filters](../docs/guides/model-aggregate-filters.md) add model/scope-owned row predicates to individual measures before optional window evaluation. Counts, existence, exact/approximate summaries and nullable extrema retain their result capabilities. Relation target/pivot filters preserve unfiltered cardinality checks; CTE discovery and explicit correlations share the existing compiler. Ordinary aggregate filters with only outer references are rejected to prevent PostgreSQL from moving the aggregate to another SELECT.

[Typed pagination](../docs/guides/model-pagination.md) provides count-free simple model pages and numbered/simple projection, set and value pages through existing request validation, decoders and execution. Model pages append the primary key; read-only result pages require explicit ordering because result uniqueness cannot generally be inferred. Count retains grouping, distinct winners and combined-result semantics; nested input windows remain intact. Simple/cursor eager loads exclude the hidden lookahead row.

[Result cursor pagination](../docs/guides/result-cursor-pagination.md) adds `CursorFor`/`ValueCursorFor` over completed record/scalar sources. Generated output fields retain `CursorScope[Result]`, exact values and nullable codecs. Required `UniqueBy` keys declare the complete result identity and supply absent ascending tie-breakers; uniqueness is an explicit caller assertion, not inferred from a joined model ID or verified with a second query. The derived-result boundary preserves source grouping, window evaluation, distinct winners, sets and input windows. Shared model/result token machinery supplies bounded nullable bidirectional pages of native slices.

[Bounded model iteration](../docs/guides/model-chunks.md) supplies `Chunk`/`EachChunked` with stable offset windows and `ChunkByID`/`EachByID` with declared primary-key traversal, including descending natural keys. Existing limits cap total delivery. Batches close rows and eager loads before callbacks; key boundaries are captured before callback mutation. Callback errors/cancellation stop work, panics follow ordinary Go propagation, and earlier effects require caller-managed consistency. No implicit snapshot, durable checkpoint or byte-size bound is promised.

[Typed upserts and batch inserts](../docs/guides/model-upserts.md) add generated `Upsert`, `CreateMany` and `UpsertMany` through one insert plan and shared transaction/returning execution. Conflict policies retain model ownership across composite field targets, named constraints, incoming/constant/NULL assignments and conditions on the stored row. Skipped upserts return explicit absence or omit that row from a typed result slice. Batches preserve per-row defaults and enforce shared bounds; decoding/cleanup failures roll back the statement. Unique constraints remain migration-owned and PostgreSQL-validated. SQL upserts and batches have explicit set-based lifecycle semantics.

[Transaction-scoped row locking](../docs/guides/row-locking.md) supports complete models, declared projections and selected values with all four PostgreSQL row strengths and explicit wait behavior. Generated locked model builders retain natural/model-specific keys. Typed `Of` scopes select preserved join inputs, while nullable, aggregate, distinct, window and set targets are rejected. Ordinary derived sources propagate row locks; outer locks do not claim to lock CTE definitions. Locked read terminals require `*database.Tx`, preserving the caller's transaction/savepoint lifetime and preventing unrestricted count/write/source composition. Parent eager loading and bounded streaming reuse the existing result runtime.

[Typed lateral joins](../docs/guides/lateral-joins.md) add correlated complete records and generated fluent report selectors. Inner/left/cross forms retain the required preceding scope, per-parent filters/windows, optional typed ON conditions and outer nullability. Standalone execution and ordinary source conversion remain unavailable for correlated records. Lateral compilation, dependency traversal and qualification reuse existing query infrastructure; the master records verification evidence.

[Typed temporal queries](../docs/guides/temporal-calculations.md) add calendar/clock extraction, truncation, explicit zones and DST resolution, typed literal interval arithmetic, exact epoch conversion and transaction time. They reuse scalar operation traversal, grouping, parameter binding and codecs. Interval-valued model codecs and result expressions are described below, with their verification recorded in the master.

Transaction-preserving CTE, derived-table, join, scalar/predicate and explicit correlation/lateral locks are implemented and verified. [Transaction correlation](../docs/guides/transaction-correlations.md) retains non-executable inner sources, nested parent visibility, nullable scopes and generated complete projections through shared query infrastructure. The complete repository gate, final race checks and consumer review passed. Multiple per-input lock clauses, expression/partial-index conflict targets, typed JSON paths and conflict calculations are verified, with evidence recorded in the master. Typed equality/range column predicates are available in a shared scope. Ordinary subqueries continue to reject accidental references to outer columns.

## Implementation slices

### Delivered conflict calculations and index targets

Conflict calculations retain the existing generated `Upsert`/`UpsertMany` terminals and model-owned conflict policies. They use the shared row-expression AST, with a distinct `ConflictRow[Model]` scope for the stored and proposed records. The implemented consumer contract has passed query and PostgreSQL/race tests, compiler-rejection checks and real-gopls inspection; the master records the evidence:

```go
q := models.QueryUsers()
u := models.UserFields()
rows := q.ConflictRows()
stored := models.UserFieldsAt(rows.Stored())
proposed := models.UserFieldsAt(rows.Proposed())
policy := query.OnConflict(u.Email, u.Level).
    DoUpdate(query.SetConflictValue(u.Age, query.Add(stored.Age, proposed.Age))).
    WhereRows(proposed.Status.Eq(models.StatusActive))
```

Generated field sets preserve exact scalar/nullable/JSON types in both records. `SetConflictValue` is a typed package function, with a sealed field-only destination contract. It avoids adding another generic scope to every field's method set. A conflict calculation cannot be used as an ordinary model predicate, and selected aggregates/windows cannot be assigned directly as row calculations. Explicit scalar subqueries retain their own SELECT phase; correlated subqueries declare the conflict records as their outer scope. Existing `Where` remains a condition on the stored model, and `WhereRows` can compare both records. Both conditions must hold when combined. The proposed row includes PostgreSQL defaults and `BEFORE INSERT` trigger changes.

Expression index inference reuses typed row keys through `OnConflictKeys`, and `TargetWhere` describes a partial index separately from the update condition. Compiler, PostgreSQL/race, consumer, generation and real-gopls checks have passed; the master records their scope and compiler resource settings. Targets contain only expressions over the indexed model; scalar subqueries, aggregates and windows are invalid there. Migrations remain the source of truth for physical indexes. Static target values use codec-validated, safely rendered SQL constants so inference remains valid under generic prepared plans; insert values, update calculations and update predicates remain bound parameters. Target constants are schema declarations visible in inspected SQL and must not contain request data or secrets. See PostgreSQL's [conflict target syntax](https://www.postgresql.org/docs/18/sql-insert.html#SQL-ON-CONFLICT) and [partial-index planning constraints](https://www.postgresql.org/docs/18/indexes-partial.html).

Acceptance must cover exact decimal calculations, nullable assignments, conditional updates, batch/default/trigger behavior, scalar and correlated subqueries, CTE discovery from assignments, target aliases (including a physical table named `excluded`), immutable policy derivation and bounds. Index acceptance must force generic plans and exercise expression/composite/partial targets, unmatched physical indexes and quoted constants. Independent negative-compilation cases must reject foreign owners, incompatible values, nullability loss and query-phase misuse.

### Delivered transaction-preserving lock composition

Lock composition preserves the existing `*database.Tx` terminal boundary. A locked source must not implement an unrestricted `RecordQuerySource`, `ProjectionSource` or `ValueQuerySource`, and a derived CTE/subquery must not silently discard a lock. The implementation reuses the existing SELECT AST, CTE planner, scope mapping, result decoders and generated projection emitter.

Typed per-input clauses are implemented and verified. The consumer shape uses both scopes obtained from the same preserved join:

```go
locked := query.SelectRecord(joined, userScope).LockRows(
    query.UpdateLock(userScope),
    query.KeyShareLock(orderScope).NoWait(),
)
items, err := locked.All(ctx, tx)
```

Provide all four strengths and independent wait policies through immutable `RowLock[Scope]` descriptors. Retain the owner in their representation so explicit Go conversions cannot erase it. Empty/invalid clauses, foreign owners and nullable outer-join inputs must fail at their appropriate static/runtime boundary. Single-clause `ForUpdate`/`Of` calls retain their existing behavior. Bound total clause validation work as well as clause count. Overlapping clauses follow PostgreSQL's strongest-lock and wait-policy precedence; cover that behavior against concurrent transactions. See PostgreSQL's [locking clause](https://www.postgresql.org/docs/18/sql-select.html#SQL-FOR-UPDATE-SHARE).

Then carry locks at the SELECT level inside CTE and subquery definitions. Public composition must retain its transaction requirement through aliases, preserved join scopes, complete model/projection selections and typed scalar/predicate boundaries. Generated adapters must share the existing selection emitter and codecs. Do not provide a conversion back to an unrestricted query or a callback that exposes such a query to consumer code. Ordinary source inputs may be lifted explicitly into the transaction-required composition family; every resulting execution terminal still requires `*database.Tx`.

Scope markers alone do not prove this boundary: public low-level generic query constructors can instantiate an exported transaction scope as their model parameter. In addition to sealed transaction-required source interfaces, the shared compiler must reject nested locks from every ordinary compilation/execution path. Only a private transaction-required path may permit them. Include a regression for a lock-carrying expression inserted into a handcrafted ordinary generic query; it must fail before SQL rather than silently acquire a short-lived lock through a pool.

Acceptance must distinguish an outer lock (which does not lock CTE definitions) from a lock declared inside the CTE. Verify actual blocking/`NoWait`/`SkipLocked` behavior using independent transactions, filter/limit placement, immutable derivation, nested rollback and lock retention through successful savepoint release. Reject illegal grouped/distinct/window/set lock scopes without removing the clauses. Validate complete result decoding, CTE dependency discovery, cancellation and resource bounds. Compile-failure cases must reject pool/session execution and leakage into ordinary source APIs; real gopls must expose the transaction requirement and generated field ownership. Only then close milestone 06.

### Delivered JSON contract

Handwritten Go payload types remain the source of truth. [Typed JSON models](../docs/guides/json-models.md) use `value.JSON[Payload]` for an immutable snapshot; decoding returns a fresh payload, including fresh maps and slices. Canonical JSON equality retains exact numeric values and array order while ignoring object-key ordering and insignificant numeric spelling. SQL NULL uses `value.Nullable[value.JSON[Payload]]`; JSON null uses a nullable payload such as `value.JSON[value.Nullable[Payload]]`. A zero, unconstructed JSON snapshot is invalid.

The first JSON increment establishes bounded strict parsing, payload-shape checks, codecs and generated whole-document fields. Duplicate/unknown object keys, lossy Unicode, missing required struct fields and non-nullable nulls must fail; ordinary optional/nullable tags and wrappers remain explicit. Custom JSON methods own their internal shape. No floating-point intermediate is allowed for dynamic JSON numbers.

Typed whole-document predicates, containment and JSON-kind inspection share the existing AST/compiler. Generated property descriptors add typed nested properties and array elements using the same Go payload declarations. Applications do not repeat JSON property strings. Missing properties, JSON null and SQL NULL remain explicit at extraction boundaries. Rust references are `JsonExprBuilder`, `JsonPathExpr`, `JsonPredicateOp` and the JSON compiler branches.

Generated `Properties()` comes from ordinary payload struct fields and `At` from declared arrays/maps. The independent JSON consumer exercises this shape:

```go
f := DocumentFields()
prefs := f.Settings.Properties()
documents, err := QueryJsonDocuments().Where(
    prefs.Theme.Scalar().Like("%dark%"),
    prefs.Labels.At("lang").Scalar().Eq("en"),
    f.Tags.At(0).Scalar().Eq("go"),
).All(ctx, db)
```

Property descriptors retain their query owner and declared payload type through nested access. Map keys retain the Go map's key type; array offsets use bounded PostgreSQL integer indices, including negative offsets from the end. JSON snapshots retain JSON null independently of missing/SQL NULL. Scalar extraction returns a nullable value and deliberately combines missing values and JSON null into SQL NULL, while existence/kind predicates expose the distinction. Custom serializers own their wire shape and receive no inferred struct properties. Payload field/tag promotion shares one bounded implementation with strict JSON value validation. Fresh discovery retains custom codec method signatures while allowing their bodies to use generated declarations.

[Conditional expressions](../docs/guides/conditional-expressions.md) now provide typed row values, CASE, COALESCE, NULLIF and codec-owned standalone parameters, alongside selected-value variants for aggregates/windows. They share validation, qualification, dependency discovery and parameter binding with the existing AST.

[Typed arithmetic and text calculations](../docs/guides/scalar-calculations.md) extend this layer with compatible numeric operations, explicit exact/approximate conversions, text transformations and row/selected comparison adapters. NULL and nominal types stay explicit. Computed model orders compose with numbered/simple pages, offset chunks, locked reads and target/pivot relation ordering; cursor calculations require declared output fields. The master records verification of this increment. Remaining JSON and conflict expression work is listed above.

[Value comparisons and join conditions](../docs/guides/value-comparisons-and-joins.md) share one binary operand node across generated column comparisons, computed WHERE/HAVING predicates and typed ON conditions. Explicit NULL-aware comparisons retain SQL semantics. Cartesian joins preserve independent input windows and existing outer nullability. Row-scalar helpers retain nested SELECT phases and correlation boundaries. PostgreSQL FULL JOIN planner restrictions remain explicit database errors; supported equality-plus-range conditions are covered by consumer acceptance. The master records this increment's verification.

[Computed query keys](../docs/guides/computed-query-keys.md) extend row `Group()` descriptors to computed grouping, partitions and DISTINCT ON. Selected `Expression.Key()` values enter `PartitionByValues`/`DistinctOnValues`, including ordinary aggregates and permitted window outputs. One bounded compiler index compares canonical SQL and encoded values, reusing grouped subexpression parameters within their SELECT. Grouping a computed value does not group its input columns individually; result cursor identities still require declared output fields.

1. Key-compatible relation descriptors, eager loading with batched queries, nested loading, and explicit lazy/batch loading helpers. Do not perform hidden database calls on Go field access.
2. Relation scopes and aggregates; soft-delete and authorization scopes must be applied deliberately to the related query.
3. Projection generation and typed joins, followed by grouping, HAVING, aggregates and subqueries.
4. CTEs, unions, window expressions, distinct operations and dialect-specific capabilities through the shared AST.
5. Upsert, streaming/chunking and row locking. Add count-free simple offset pagination alongside numbered/cursor pagination, and complete typed projection page helpers. Integrate lifecycle semantics in milestone 07, rather than promising events before that pipeline exists.

Generated metadata and runtime expression composition must converge on the same AST. A raw SQL expression uses an explicit unsafe/raw boundary with a typed decoder; it does not claim compile-time SQL semantic validation.

## Failure and performance behavior

[Interval queries](../docs/guides/interval-queries.md) extend temporal calculations with generated interval fields/codecs, nullable summaries, arithmetic, differences, component extraction and dynamic shifts. Direct/through/aggregate relation grouping follows PostgreSQL interval equality while preserving stored components. The master records this increment's verification separately from the remaining milestone work.

Batch eager loading avoids N+1 queries and observes configured key/batch limits. Nested relations have explicit depth limits. Close streams when callbacks fail or contexts cancel. Cursor pagination uses a deterministic unique tie-breaker. Validate incompatible projection mappings before executing SQL whenever metadata makes the mismatch knowable.

## Acceptance

Compile-test incompatible relation keys, wrong owners, wrong projection types, and aliases outside scope. Test self-relations, nullable belongs-to, empty has-many, duplicate join rows, many-to-many pivots, scoped eager loads, soft-deleted related records, aggregate nullability, stable pagination, CTE/window results, upsert conflicts and canceled streams. Assert query counts for eager-loading fixtures. Apply the [common gate](README.md#common-completion-gate).

## Completion review

The following maps the milestone's required behavior to independent consumer evidence. The [master's completion record](00-master-architecture-and-parity.md#milestone-06-completion-evidence) owns execution results: the complete repository gate and final relevant race checks passed, including independent consumers, generation and real gopls. The consumer experience was reviewed before beginning milestone 07.

| Required behavior | Concrete consumer evidence |
| --- | --- |
| Typed relation keys, owners, targets and scopes | [Handwritten declarations](../tests/fixtures/consumer/models/relations.go), generated field descriptors, and [required compiler failures](../tests/fixtures/consumer/types_test.go) |
| Self-relations, nullable belongs-to, empty collections, natural keys, has-one cardinality and scoped eager loading | [Direct relation acceptance](../tests/fixtures/consumer/model_relations_postgres_test.go) checks returned models, explicit loaded states, failed-load isolation and exact query counts |
| Many-to-many pivots, duplicate edges and nested target/pivot loading | [Through relation acceptance](../tests/fixtures/consumer/model_through_postgres_test.go) checks concrete target/pivot records, SQL ordering, ambiguous targets and fetch/attachment budgets |
| Typed aggregates and SQL NULL | [Aggregate acceptance](../tests/fixtures/consumer/model_aggregates_postgres_test.go) checks empty groups, exact sums beyond int64 and aggregate query counts |
| Complete projection records, aliases, inner/outer/self joins and subqueries | [Projection](../tests/fixtures/consumer/projectionqueries/), [join](../tests/fixtures/consumer/joinqueries/), [subquery](../tests/fixtures/consumer/advancedqueries/) and [correlation](../tests/fixtures/consumer/correlations/) fixtures check record types, nullability, visibility and cardinality |
| CTEs, recursion, grouping, HAVING, sets and windows | [CTEs](../tests/fixtures/consumer/ctes/), [recursive queries](../tests/fixtures/consumer/recursivequeries/), [sets](../tests/fixtures/consumer/setqueries/), [windows](../tests/fixtures/consumer/windowqueries/) and [window frames](../tests/fixtures/consumer/framequeries/) execute typed compositions against PostgreSQL |
| Numbered/simple/cursor pagination with stable result identities | [Model pagination](../tests/fixtures/consumer/model_pagination_postgres_test.go), [result pages](../tests/fixtures/consumer/pagequeries/) and [result cursors](../tests/fixtures/consumer/cursorqueries/) check page contents, forward/backward traversal, NULL endpoints and invalid cursors |
| Bounded iteration, cancellation and stream cleanup | [Chunk acceptance](../tests/fixtures/consumer/chunkqueries/chunks_postgres_test.go) checks callback ownership, eager batch counts, changing predicates, callback errors and invalid inputs; locked streaming is covered by the lock fixture |
| Upserts, batch defaults, conflict expressions and index targets | [Upsert acceptance](../tests/fixtures/consumer/upsertqueries/) checks returned rows, concurrent conflicts, transformed database defaults, nullable assignments and partial/expression index matching |
| Transaction-required locks through derived records, joins, CTEs, scalars and correlations | [Lock acceptance](../tests/fixtures/consumer/lockqueries/) and [transaction composition](../tests/fixtures/consumer/transactionqueries/) observe contention from independent transactions, savepoint rollback, cancellation and rejected targets |
| Reproducible generated APIs and editor discovery | [Generator acceptance](../internal/generate/projection_test.go), recursive generation checks and [real-gopls acceptance](../internal/agent/gopls_acceptance_test.go) exercise generated projections, concrete signatures and definitions in the consumer workspace |

The consumer review preserves handwritten model/relationship declarations, complete declared projection types and native result slices. Alias scopes are explicit only where SQL has multiple record occurrences; ordinary model queries retain generated fluent methods. Transaction-required composition shares the ordinary AST, compiler, decoding and scope helpers while retaining `*database.Tx` at every executable boundary.

Soft-delete defaults and lifecycle-aware relationship writes depend on the pipeline specified in [07](07-model-lifecycle-events-and-audit.md). Current relation tests prove explicit target/pivot filtering; they do not prove automatic soft-delete filtering. Milestone 07 must add that integration and its acceptance coverage before the full framework can be complete. Attachment/metadata/translation eager-loading integrations remain owned by [18](18-imaging-and-model-extensions.md). Compiler and generated-code resource measurements remain an explicit [production developer-experience gate](24-production-hardening-and-release.md#implementation-slices).

### Completion checklist

- [x] Typed direct/through relations, self/natural/nullable keys, scoped/nested eager loading, aggregate slots and asserted batch query counts.
- [x] Complete projections, typed join scopes, ordinary/correlated scalar queries, CTEs/recursion, sets/distinct, grouping/HAVING and windows through one SQL compiler.
- [x] Typed expression, temporal, interval and JSON capabilities with explicit NULL semantics and bounded compilation.
- [x] Numbered/simple/cursor result pages, deterministic identity requirements, bounded iteration and cancellation/cleanup behavior.
- [x] Typed insert/upsert batches, conflict calculations/index targets, transaction-only row locks and preserved locks across CTE/join/subquery/correlation/lateral boundaries.
- [x] Complete repository gate, all 399 required compiler failures, current generation, documentation and real-gopls checks.
- [x] Final query/language-client and affected PostgreSQL consumer race checks, with the execution conditions recorded in the master.
- [x] Consumer experience reviewed; later lifecycle, extension, tooling and operational integrations remain explicitly owned by their required milestones.
