# Go-way ORM breaking-change notes

Prepared for the lead's Stage 10 CHANGELOG, against worktree base `68885959` plus
accepted lane B changes. This is a migration list, not a claim that generator or
legacy runtime retirement has already landed. Publication requires the serial
caller cleanup, frozen legacy evidence and integrated acceptance.

## Consumer changes

- Replace generated model queries, field sets, drafts, changes and relation sets
  with `orm.Define` declarations, typed columns, assignment lists, `orm.Changes`
  and relation variables. New model declarations do not require generation.
- Replace legacy model-query `RequireFirst` with `Require`, including locked
  model queries. Rows result adapters retain `RequireFirst`.
- Declare managed timestamps with `CreatedAtOf` and `UpdatedAtOf`. An ordinary
  `TimeOf` binding on a managed field is rejected at startup. Keep NoTimestamps
  behavior and explicit constructor conflicts.
- Every mutated field uses an Input carrier, even when input and storage types
  are identical. Setters take input types; comparisons use stored types. Invalid,
  orphan or conflicting Mutate/Access methods now fail declaration validation.
  Named methods and explicit accessor calls remain ordinary Go.
- Keep the primary role through Key, NumberKey and TextKey adapters. PrimaryOf
  is needed only for primary-key insert-select mappings. InputKeyOf instead binds
  a mutation input to the single primary declaration; distinct input suppresses
  automatic UUID synthesis. InputKeyOf and PrimaryOf are mutually exclusive.
- Expose a declaration-bound SoftDeletable token, such as Users.Soft, for every
  force-delete path. The token does not broaden query visibility. Zero or
  foreign-declaration tokens fail before SQL.
- Source MapInsert/MapUpdate targets enforce roles statically. Both reject input
  columns and managed update timestamps; MapUpdate also rejects primary keys.
  CreatedAt remains eligible. Column wrappers/conversions do not grant eligibility.
- Hook factories return `orm.Hooks[M]`; draft callbacks take `*orm.Write[M]`.
  WriteGet/WriteNullableGet/WriteIsSet/WriteSet/WriteUnset are package functions.
  WriteUnset means omitted. ChangeOf/NullableChangeOf return lifecycle.FieldChange.
- Register native hooks/observers together for each application instance. Mixed
  generated/native observer adapter families for one model are rejected. Keep
  after-commit outcome handling and explicit set-write acknowledgement.
- Global scope sources are lazy declaration options; resolvers return additive
  predicates and errors. Bypass APIs are package functions, including relation
  target and pivot variants. Failed resolution never permits unscoped execution.
- Complete-model Raw reads dispatch retrieval callbacks but bypass global scopes.
  DTO/scalar projections and write hydration skip retrieval. Preserve caller
  authorization and tenant restrictions at raw boundaries.
- Conflicts/upserts, lookup writes, source writes and several read terminals move
  to package functions. Use typed conflict targets and EXCLUDED expressions.
  CreateOrFirst replaces insertion-first InsertOrFirst. Unknown outcomes remain
  failures, with no success coercion, definite rollback claim or automatic retry.
- Replace generated projection builders with `rows.Define` plus typed mappings.
  Keep concrete DTO types and DTO generation separately. `projection dto=true`
  becomes rows declarations plus `foundry:dto` at the serial barrier. Persistence
  models remain excluded from response DTO shapes.
- Query/row/aggregate/window/cursor phases remain distinct. Use the adapter for
  the exact result family, retaining transaction requirements for locked records.
  Group/having, sets, CTEs, window expressions and JSON paths move to rows/expr.
- JSONFieldOf replaces generated model JSON property selectors. JSON tag,
  optional/null/pointer and `,string` semantics remain validated. Retain DTO-facing
  path naming and validation support.
- Datatable sources, sorting and filtering consume rows/expr types. Preserve
  authorization, tenant predicates, Spec IDs, DTO descriptors and exports.
- Register explicit tables using application.WithTables before providers start.
  No runtime package discovery occurs. Direct assembly can use orm.Check/rows.Check.
- Replace generated reference calls with orm.IdentityOf(...).ReferenceOf(...).
  Models required by model.Identifiable still need a handwritten FoundryIdentity
  delegate when generated methods are removed; an explicit reference alone is
  insufficient for that interface.
- Factories build assignment slices. Seeders keep seed.Definition and the supplied
  transaction. Model scaffolds emit handwritten declarations and Tables helpers;
  DTO/enum/config generation and migration/seeder commands remain supported.
- Generated per-field hover notices retire with model generation. Handwritten
  field, method and declaration documentation becomes the IDE documentation owner.
  Wrong mutator signatures, undeclared persisted fields and unsupported field
  types are startup diagnostics; model/value/role restrictions remain static.
- Public pagination aliases now share the common SQL contract owner. Reflected Go
  defining-package names can change; public HTTP cursor schema identities and
  encoded cursor bytes remain fixed.

No persisted table/column/key rename, schema migration, JSON field rename, manifest,
OpenAPI or TypeScript byte change is intended by this migration. Encryption identity
and extension storage ownership must remain pinned across application refactoring.

## Main H1 compatibility changes

These accepted main changes must accompany the branch migration. Their source is
main's `CHANGELOG.md` and `docs/evidence/main-h1-error-containment.md`, inspected
read-only in the Foundry-Go-mainfix worktree.

- Canonical JSON map-key `MarshalText` panic or `runtime.Goexit` now returns HTTP
  **500 instead of 400**, relative to `e1c2356a`. These are internal failures.
  Ordinary returned key-codec errors remain invalid input, HTTP **400**. Exclude
  this correction from claims that all HTTP error statuses and bytes are unchanged.
- Callback and codec failures replace application error text with safe framework
  diagnostics without formatting the application error. Causes remain available
  through `errors.Is` and `errors.As`; classification precedence is preserved.
  Switch consumers and tests from error-string matching or assumptions about the
  error's root concrete type to sentinel identity with `errors.Is` and cause-type
  inspection with `errors.As`.
- Move native PostgreSQL registrations out of query rewriters into
  `postgres.Config.RegisterTypes`, whose signature is
  `func(context.Context, *postgres.TypeRegistry) error`. Its setup-only registry
  is callback-scoped and cannot be retained. Callback or registry-operation
  failure prevents pool admission. Registration inside a rewriter, including
  same-pointer registration, rejects the statement and discards the connection
  without automatic retry. See the
  [registration migration contract](../guides/orm-migration.md#move-postgresql-type-registration-into-connection-setup).

## Integration prerequisites for this guide

- **Lane A / lead:** the requested `orm.LoadFrom` and `LoadMissingFrom` are present
  in the accepted `laneA-full2-on-fdaff938.patch`, but absent from this worktree.
  Integrate before publishing their compiled examples. Their signature accepts the
  query and existing parents so the query's relation tree and limits survive.
- **PostgreSQL owner / lead:** `database/postgres.Config` has no RegisterTypes field
  in this worktree. Main H1's accepted contract is documented above, but its
  compiled example stays blocked until H1 reaches the branch. Integrate the
  implementation before publishing that example.
- **Lead / generator owner:** preserve native persistence-model recognition when
  removing model directives and generated methods. Freeze legacy oracle evidence
  before retiring the old example/oracle imports used by the migration package.
  Later removal must preserve these compiled examples' historical meaning, not
  leave a second hidden model generator.

## Other guides with stale sections

No other guide is edited in this slice. These are source-inspected migration tasks,
not a blanket instruction to delete each page. Keep runtime guarantees and retained
DTO/enum/client documentation. References to DTO ValidationFields or request
FormDraft alone are not legacy ORM usage.

| Guide | Sections/examples to migrate |
| --- | --- |
| [Model generation](../guides/model-generation.md) | “Model declarations”, “Typed queries and drafts”, model/projection generation ownership |
| [Queries](../guides/model-queries.md), [writes](../guides/model-writes.md), [batch writes](../guides/model-batch-writes.md) | Generated Query/Fields/Draft calls; create/update and set-write examples |
| [Mutators](../guides/model-mutators.md), [changes](../guides/model-changes.md), [hooks](../guides/model-hooks.md) | Input/storage declarations, generated changes, provider observer and callback signature tables |
| [Retrieval](../guides/model-retrieval.md), [timestamps](../guides/model-timestamps.md), [soft deletes](../guides/model-soft-deletes.md) | Annotation setup, typed writes/visibility, force-delete token |
| [Global scopes](../guides/model-global-scopes.md) | “Declaring scopes”, “Removing scopes” |
| [Relations](../guides/model-relations.md), [relation writes](../guides/model-relation-writes.md), [relationship filters](../guides/model-relationship-filters.md), [relation aggregates](../guides/model-aggregates.md) | Generated relation sets, attach/sync drafts, filters and loading examples |
| [Upserts](../guides/model-upserts.md), [lookup writes](../guides/model-lookup-writes.md) | Conflict fields, expression targets, update callback draft types |
| [Insert from query](../guides/model-insert-from-query.md), [source writes](../guides/model-source-writes.md) | Generated mapping builders, fixed inputs, source deletion |
| [Projections](../guides/model-projections.md) | “Declare the result”, “Public report DTOs”, generated Select helpers |
| [JSON models](../guides/json-models.md) | Model annotations, generated path selectors and query/write examples |
| [Joins](../guides/model-joins.md), [subqueries](../guides/model-subqueries.md), [correlations](../guides/model-correlations.md), [lateral joins](../guides/lateral-joins.md) | query aliases, generated FieldsAt and record/projection adapters |
| [CTEs](../guides/model-ctes.md), [recursive CTEs](../guides/model-recursive-ctes.md), [set operations](../guides/model-set-operations.md), [distinct](../guides/model-distinct.md) | Model/row/value source families and complete-record read adapters |
| [Windows](../guides/model-windows.md), [window ranges](../guides/window-ranges-and-names.md), [aggregate filters](../guides/model-aggregate-filters.md) | query expression and generated field examples |
| [Computed keys](../guides/computed-query-keys.md), [conditional expressions](../guides/conditional-expressions.md), [scalar calculations](../guides/scalar-calculations.md), [field computations](../guides/typed-field-computations.md), [value comparisons](../guides/value-comparisons-and-joins.md) | Row/selected phase examples using old query types |
| [Interval queries](../guides/interval-queries.md), [temporal calculations](../guides/temporal-calculations.md) | Generated field/draft calls and expression families |
| [Pagination](../guides/model-pagination.md), [result cursors](../guides/result-cursor-pagination.md), [chunks](../guides/model-chunks.md) | Generated model and query result traversal examples |
| [Row locking](../guides/row-locking.md), [transaction composition](../guides/transaction-query-composition.md), [transaction correlations](../guides/transaction-correlations.md) | Generated queries and transaction-record adapters; retain transaction guarantees |
| [Pruning](../guides/model-pruning.md) | prune.Model, generated query bounds and lifecycle/mass registrations |
| [Extension slots](../guides/model-extension-slots.md), [extensions](../guides/model-extensions.md) | Annotation/owner identity, generated slot loading and query.IdentityOf |
| [HTTP model binding](../guides/http-model-binding.md), [scoped binding](../guides/scoped-model-binding.md), [HTTP pagination](../guides/http-pagination.md) | Generated ByKey/ThroughRelation/query examples |
| [Authentication](../guides/authentication.md) | “One provider, multiple guards” generated QueryUsers lookup |
| [Encryption](../guides/encryption.md), [password hashing](../guides/password-hashing.md) | Model annotation examples; retain cryptographic/secret semantics |
| [Database routing](../guides/database-routing.md) | “Primary and read database pools” generated QueryUsers example |
| [Developer tooling](../guides/developer-tooling-and-testing.md) | “Typed factories and explicit fakes”: Factory draft type |
| [Idempotent operations](../guides/idempotent-operations.md), [typed API workflow](../guides/typed-api-workflow.md) | Generated drafts in transactional/domain examples |
| [Expanded validation](../guides/validation-expanded.md) | “Batched model lists”: generated UserFields; retain DTO validation examples |

The current model-scaffolding guide already describes native tables and roles;
its generated-codec and field-doc notes still need the final retirement wording.
`database/orm/README.md` also retains historical “deferred” statements for upsert,
interval aggregates and JSON expression work that has since shipped. The lead can
consolidate those during Stage 10 rather than rewriting them in this docs slice.
