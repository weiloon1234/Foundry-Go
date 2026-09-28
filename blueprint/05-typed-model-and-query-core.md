# 05 — Typed model and query core

## Purpose and prerequisites

Prerequisite: [04](04-database-runtime-and-migrations.md). Deliver Foundry's central promise: model fields, queries, mutations, and results preserve their types through execution.

Rust references: `src/database/model.rs`, `query.rs`, `ast.rs`, `compiler.rs`; `foundry-macros`; `tests/database_acceptance.rs`, `tests/derive_ui.rs`, `docs/guides/database.md`. Usage reference: Rust Starter's `src/domain/models/user.rs` and `src/domain/services/user_service.rs`.

## Public type design

- `Query[M]`, `Predicate[M]`, and ordering tokens carry the model owner. Typed field expressions retain both owner and value type.
- Generate model-specific query entrypoints, create/update drafts, setters, field accessors, and row decoders. Operator-specific field wrappers expose text operations only for text, numeric operations only for supported numerics, and null operations only for nullable fields.
- UUID identities use the model owner in their type. Natural/custom keys preserve a named Go key type and codec rather than being forced into UUIDs or raw strings.
- Generate create and patch representations that distinguish absent, present zero, and present null. A nullable field value and an omitted patch field are different types/states.
- Enums retain their named types and validate serialized/database values. Go casts and incoming bytes are runtime validation boundaries, not proof of enum membership.
- Query construction is copy-on-write: deriving one query does not mutate another. Terminal operations take an explicit executor and context. Internal buffers must not alias across derived builders.

Delivered read shape (generated accessor, not a mutable exported field registry):

```go
query := models.QueryUsers().Where(models.UserFields().Email.Eq(email))
users, err := query.All(ctx, db)
```

`UserFields().ID.Eq(orderID)` and a user query with an order predicate must fail compilation. Generated setters such as `SetEmail(string)` must reject integers. Optional fields expose typed clear/unset operations rather than accepting `any`.

## Execution and mutation contract

The delivered core is documented in [typed database values](../docs/guides/database-codecs.md): `database/codec` owns concrete bind/scan contracts, `decimal.Decimal` owns exact finite values and bounded arithmetic, and generated enum SQL interfaces delegate to their generated codec. The generator emits ordered decimal fields and concrete draft setters. [Model reads](../docs/guides/model-queries.md) connect these codecs to a shared SQL compiler and complete hydration. [Model writes](../docs/guides/model-writes.md) add typed create/update/delete, required/default validation, returning hydration and composable transaction scopes. [Pagination](../docs/guides/model-pagination.md) adds bounded numbered and cursor pages.

Codec decisions: named integer widths and SQL signed range are checked at runtime; decimals never accept float sources; NULL requires `value.Nullable`; dates, wall time and instants remain distinct; temporal SQL codecs reject sub-microsecond input. A failed column decode leaves its destination unchanged, while generated whole-row hydration must still discard all partial row state. Exact decimals normalize display scale and serialize as JSON strings. Constrained SQL numeric columns can still round according to their schema, so model/schema precision rules must be explicit.

Use one internal AST for reads and writes and one PostgreSQL compiler. Bind data as parameters; column/table identifiers come from validated declarations. Private type erasure at the SQL driver boundary must not leak into ordinary public model APIs.

Provide `All`, `First`, `Find`, `Count`, `Exists`, offset pagination and stable cursor pagination. `First`/`Find` use an explicit not-found result contract; required variants return a typed not-found error. Do not use a zero-valued model as a missing-record sentinel.

Delivered read decisions: generated model-specific query wrappers preserve concrete primary-key types through fluent derivation, while shared `query.Query[M]` owns compilation/execution. `First`/`Find` return `value.Optional[M]`; required variants report `database.NotFound`. `Limit(0)` is empty. `Count` and `Exists` honor the selected window, and callers count an unpaginated base for a total. `Each` streams complete models. `All` discards the entire collected result on hydration failure. Default first-row ordering uses the primary key; stable business orderings must declare their tie-breaker. Model reads have no partial-selection API.

Create validates required fields before SQL. Explicit mutation setters prevent accidental full-struct overwrites and mass assignment from request DTOs. Model query hydration always reads all persisted fields; selected subsets use the projection system in milestone 06. Exact decimals never pass through floating point.

Delivered write decisions: generated wrappers retain concrete draft/key types and delegate to one shared mutation pipeline. A `database.Transactor` opens a transaction or a nested savepoint. Single-model updates/deletes require primary equality and reject read pagination/ordering; empty patches and primary-key mutation fail. Missing/multiple/malformed returned rows roll back. A database-default marker permits omission without copying SQL default expressions into models. UUID keys are prepared only when omitted. Typed `WriteError[M]` retains a complete reconciliation candidate when commit confirmation is uncertain or after-commit work failed. Model observers and outbox dispatch remain milestone 07 work.

Delivered pagination decisions: numbered pages use separate count/row queries through an explicit executor; a caller-owned repeatable-read transaction supplies a common snapshot when needed. Cursor pages use one limit-plus-one query, generated typed getters/codecs, null-aware lexicographic predicates and a primary-key tie-breaker. Canonical order survives backward traversal. Versioned model-owned tokens are bounded and validated against query/model identity and field codecs. Tokens contain encoded sort values, are not authorization credentials and provide no cross-request snapshot. Existing limits/offsets and repeated sort columns fail rather than being silently discarded.

## Implementation slices

1. Extend generator prototypes into model metadata, codecs and PostgreSQL hydration.
2. Implement predicate/order AST nodes and CRUD compilation with bound parameters.
3. Add typed create/patch states, required-field checks, defaults, and returning rows.
4. Add pagination, terminal helpers, query cloning, and structured database errors.
5. Route every write through a single mutation pipeline ready for milestone 07. Lifecycle integration must not require separate SQL implementations per API.

## Failure and acceptance

Compile-fail cases: wrong owner/value/key, string-only operator on a numeric field, assigning null to a non-nullable field, and using a projected record as a persisted model. Runtime cases: malformed stored enums, null decoding, missing required fields, missing rows, numeric precision, overflow, canceled scans, SQL injection strings, empty result sets, and cursor ties.

Test PostgreSQL behavior and deterministic SQL compilation. Verify gopls completion shows generated methods in the consumer package. Apply the [common gate](README.md#common-completion-gate).

## Completion checklist

- [x] Generated model metadata, field codecs and complete hydration: [generator](../internal/generate/emit_model_query.go), [codec tests](../database/codec), and [PostgreSQL model reads](../tests/fixtures/consumer/model_query_postgres_test.go).
- [x] Shared bound CRUD compilation and immutable predicate/order derivation: [compiler](../database/query/compiler.go), [mutation compiler](../database/query/compile_mutation.go), and their package tests. Model-specific generated files contain no SQL compiler.
- [x] Typed create/patch drafts, required/default rules and complete returning rows: [generated write adapter](../internal/generate/emit_model_mutation.go), [write acceptance](../tests/fixtures/consumer/model_write_postgres_test.go), and [outcome tests](../database/model_mutation_test.go).
- [x] Terminal reads, explicit missing results, structured database errors, numbered pages and stable cursors: [execution](../database/query/execution.go), [pagination acceptance](../tests/fixtures/consumer/model_pagination_postgres_test.go), [failure tests](../database/model_pagination_test.go), and [cursor boundary/fuzz tests](../database/query/pagination_test.go).
- [x] One transactional mutation pipeline shared by all generated model writes: [execution boundary](../database/query/execute_mutation.go) and `database.Transactor` composition. Milestone 07 will integrate typed lifecycle bridges into this path; model hooks are not claimed here.
- [x] Independent consumer, negative compilation, generation, docs, race and real-gopls gates: [master verification evidence](00-master-architecture-and-parity.md#milestone-05-pagination-and-completion-evidence). A declared partial response record cannot be submitted as a model draft; actual generated projection records and their additional type-safety tests belong to milestone 06.

Consumer experience reviewed: handwritten domain models own types; generated fields, drafts and fluent queries remain available to ordinary Go tooling. Foundry owns SQL, binding, hydration, transaction scopes and pagination mechanics. Callers own database schema migrations, explicit authorization predicates and any required cross-query transaction snapshot. No starter project is introduced.
