# E01 — Slot declarations and generation

Prerequisites: accepted milestones 06, 07, 18 and 20. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Existing behavior and additions

Relation and aggregate slots already use this shape. Discovery recognizes
`relation.One`/`Many`/`Through`/`Value` fields by package path and type name in
[model discovery](../../internal/generate/discover.go). The
[relation](../../internal/generate/emit_model_relations.go) and
[aggregate](../../internal/generate/emit_model_aggregates.go) emitters then bind
the handwritten `DefineRelations`/`DefineAggregates` result through a `query.Memo`.
[Global scopes](../../internal/generate/discover_model_scopes.go) show how a
handwritten `Define...` method is validated with a source diagnostic.
[Generated auditing](../../internal/generate/emit_model_audit.go) shows a
generated declaration that the application registers through typed observers.

Extension declarations today are package variables built with
`extensions.DefineOwner`, `translations.Define`, `attachments.Define` and
`metadata.Define`. The application adds each registration to
[`FeatureDeclarations`](../../application/feature_declarations.go) and composes
`Cleanup` calls in its own Deleted hook. E01 generates that composition from the
model. It reuses those constructors and adds no second registry, table or
cleanup implementation.

## Slot field types

| Field type | Owning package | Stored in | Loaded value |
| --- | --- | --- | --- |
| `translations.Text` | [translations](../../translations/definition.go) | `foundry_model_translations` | one immutable `translations.Values` snapshot |
| `attachments.One[M]` | [attachments](../../attachments/definition.go) | `foundry_attachments`, variants and a storage disk | optional `attachments.File[M]` |
| `attachments.Many[M]` | attachments | same | ordered `[]attachments.File[M]` |
| `metadata.Value[V]` | [metadata](../../metadata/definition.go) | `foundry_model_metadata` | optional `V` |

Each slot distinguishes not loaded, loaded empty and loaded present, like
[`relation.One`](../../database/relation/loaded.go). The zero value is not loaded.
Reading a slot performs no I/O. Loaded collections are copied at construction and
retrieval. Arbitrary maps or slices inside a decoded metadata `V` keep ordinary Go
value-copy semantics, as relation targets do. Slots refuse implicit JSON
serialization, like `translations.Values` and `attachments.Attachment`; responses
map loaded values into DTOs explicitly.

`attachments.File[M]` is an owner-free view of a ready file: ID, collection,
optional locale, detected info, position, creation time, properties and ready
variants. `attachments.Attachment[M, K]` embeds it, so the existing exported method
set is unchanged. The `M` argument of `One`/`Many` must be the enclosing model;
another model fails generation, and the typed file ID `attachments.ID[M]` keeps a
gallery file of one model from being detached from another.

Only framework loaders construct loaded slot values, through a sealed constructor
like `relation.FoundryCollection`. Exported test constructors are added only if a
consumer fixture proves that response-mapping tests need them.

## Model declaration

A slot field is exported and non-embedded. It accepts no column/default tags. The
only accepted tag option is `foundry:"name=<name>"`, which pins the stored name;
the default is the snake_case Go field name (`Galleries` → `galleries`). Stored
names use the existing semantic identifier rule and must be unique across all
slots of one model. `foundry:"-"` excludes the field, which is then not a slot.

`DefineExtensions() <Model>ExtensionSet` uses a value receiver and the exact
signature, validated like `DefineGlobalScopes` with a diagnostic at the method.
It is required when the model has any attachment slot and optional otherwise.
The generated set type has one field per slot:

| Slot | Set field type | Zero value |
| --- | --- | --- |
| `translations.Text` | `translations.Options` | default `MaxBytes`, no required locale |
| `attachments.One[M]` / `Many[M]` | existing `attachments.Policy` | invalid; generation reports a missing method or startup reports the slot |
| `metadata.Value[V]` | `metadata.Spec[V]` (`Version`, `JSON`) | version 1 and an inferred contract |

Cardinality comes from the slot type. A policy's `Cardinality` must be zero or
match; generation supplies it. Metadata contract inference covers generated DTO
descriptors (`ArticleSEOJSON()`), the scalar descriptors in `contract` and
`json.RawMessage` through `contract.DynamicJSON()`, plus types with a
`JSONContract` method. Any other `V` requires an explicit `JSON` descriptor:
generation fails when the model has no `DefineExtensions`, and assembly fails when
the method leaves that entry empty, because generation cannot evaluate the method
body. `translations.Options` gains `Require`, an `i18n.LocaleRequirement`
(`OptionalLocales` by default, `DefaultLocale` or `AllLocales`), shared with the
locale-map validation rule. It is enforced by E03's input rule and exact
synchronization, not by lower-level writes.

Existing per-owner limits apply: 64 translated fields and 256 metadata keys per
owner, plus attachment collection limits. They are enforced when the managers
are constructed at assembly, not by generation.

## Owner identity

Generation declares one owner per model with slots. Its owner name and storage
model both default to the table name. The storage model then equals today's
`DefineOwner` default, so data written through an existing hand-declared owner
with the same name keeps its scope. E01 proves that equality with a regression
test before documenting migration from hand-written declarations.

The model directive gains `extension_owner=<name>`, which pins both the owner name
and the storage model. Declare it with the old table name before renaming a table
that owns slots. `PreviousModels` recovery for an undeclared earlier rename stays
on the explicit owner path and the existing `rescope` commands.

## Generated API

For model `Article` with key `K`, generation emits:

- `ArticleExtensionSet`, the policy type returned by `DefineExtensions`.
- `ArticleExtensionSlots`, with one descriptor per slot:
  `translations.TextSlot[Article, K]`, `attachments.OneSlot[Article, K]`,
  `attachments.ManySlot[Article, K]` and `metadata.ValueSlot[Article, K, V]`.
  Each wraps the existing `Field`, `Collection` or `Key` declaration and exposes it
  for advanced calls. Generated getters/setters bind the concrete struct field.
- `ArticleExtensions() ArticleExtensionSlots`, built once through `query.Memo`;
  `DefineExtensions` runs once per process and must not call this accessor.
- `(ArticleExtensionSlots).From(slots.Runtime) ArticleExtensionSlots`, returning
  a copy whose descriptors borrow the runtime's managers.
- `ArticleExtensionDeclaration() slots.Declaration` and one package-level
  `FoundryExtensions() []slots.Declaration` listing every slot-owning model of the
  package in model name order.

`From`, and `All` from [E02](02-slot-loading-and-reads.md), are reserved. A slot
with either name fails generation with a dedicated diagnostic; other collisions
use the existing generated-name check, and two slot models of one package with the
same owner name fail generation. Generated names participate in the normal overlay type check and
publication transaction.

## Binding package and application assembly

The binding package (working name `extensions/slots`) sits above the three
extension packages to keep imports acyclic. It owns:

- `Runtime`, the borrowed translations, attachments and metadata managers. Each
  manager is optional by feature. Binding or using a slot whose manager is absent
  reports `fault.Missing` naming the slot kind; it never degrades silently.
- `Declaration`, an immutable envelope holding the generated owner, per-kind
  registrations and a typed cleanup-observer registration.

The application builder gains `Models(declarations ...slots.Declaration)`.
Declarations are static values, so they are captured at build time like job
declarations; the services-dependent `FeatureDeclarations` constructor cannot
carry them, because database observers must be registered during the
registration phase, before the pool binds its frozen observer set. Merging expands
each declaration into the existing owner, translation, attachment and metadata
slices, so duplicate detection and manager construction stay in their current
owners. A declaration that needs a disabled feature fails `Build` before any
resource is acquired, naming its owner: every slot needs extensions, translated
slots need locales, and attachment slots need attachments and storage.
`Services.ModelExtensions()` returns the `Runtime`.

## Automatic cleanup

Each declaration registers a hard-delete observer through the generated
`Register<Model>Observer` on the extension store's database pool. Its Deleted
callback runs metadata, translation and attachment `Cleanup` for every configured
manager, so data written through the explicit APIs with the same owner is removed
too. It passes the prior typed reference and lifecycle operation, in the same order
as the [profiles consumer](../../tests/fixtures/consumer/profiles/profile.go). A
declared slot kind whose manager is absent fails observer construction.
Soft deletion preserves data. Parent rollback preserves it and suppresses storage
deletion, as today. Bulk deletes skip observers, so existing orphan inspection and
pruning still apply.

Slot-owning models must be written through the extension store's database,
because cleanup joins the owner's transaction. The guide states this requirement.
An application that already composes `Cleanup` manually must remove that hook when
adopting generated declarations. Acceptance verifies that a duplicate cleanup is
harmless.

## Documentation for editors and agents

Generated descriptor and set fields carry doc comments naming the slot kind,
stored name, storage table, and the load and save calls. `--field-docs` notices
extend from persisted columns to slot fields. An agent reading the model file then
sees each slot's storage and usage beside the field, through the existing
[field documentation](../../internal/generate/field_documentation.go) publication
and recovery path.

## Acceptance

- Generation: fresh, repeated and check-only runs are deterministic. Diagnostics
  with source positions cover: another model as `One`/`Many` argument; column or
  unknown slot tags; duplicate or invalid stored names; reserved names; a missing
  or wrongly typed `DefineExtensions`; attachment slots without policy; and a
  metadata `V` without an inferable contract.
- Compile-fail cases in the [existing catalog](../../tests/fixtures/consumer/compiler_batch_test.go):
  a policy of the wrong kind in the set, and one model's descriptor in another
  model's query or write.
- Assembly: `Models` registers owners and slots; disabled features and duplicates
  fail before resources are acquired; two independently built applications stay
  isolated.
- PostgreSQL cleanup: hard delete and force delete remove rows and files, soft
  delete and restore preserve them, parent rollback preserves them, and duplicate
  manual cleanup is harmless.
- Scope equality between a generated owner and the equivalent hand-declared owner.
- Real gopls completion and hover for `models.ArticleExtensions().` and slot
  fields, registered in the [editor probes](../../internal/agent/gopls_acceptance_test.go),
  plus field-notice publication and recovery.

Finish all implementation, documentation and tests, then execute the common
milestone gate.
