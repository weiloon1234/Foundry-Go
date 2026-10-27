# E02 — Slot loading and reads

Prerequisites: E01. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Native slot loading

Declare model slots with `orm.SlotOf`, bind the table key and slot through
`extensions.BindSlot`, and declare a native translation, attachment or metadata
handle. `handle.From(manager).Relation(slot)` supplies a typed eager relation to
`Query.With`. The ORM imports no extension package; its shared loader owns relation
validation, depth, batching and publication.

Fetch returns one loaded value per parent in input order. An unbound handle fails
before parent SQL. `LoadMissingFrom` skips loaded slots. Fetched values count
against query-owned relation row/depth budgets; failures publish no partial parents.
Cancellation, loaded-empty state and duplicate-slot validation remain shared with
ordinary relations.

Complete-model reads, pagination and streaming can load slots. Existing parents
use `LoadFrom` or `LoadMissingFrom` with an explicitly configured query and no
parent SELECT. Count and existence terminals never load slots. Nested relation
queries carry slot relations through the same owner. The
[articles declaration](../../tests/fixtures/consumer/articles/tables.go) is the
executable shape; there is no generated extension namespace.

## Transactions and snapshots

When the executor is a `*database.Tx` of the extension store's pool, fetch joins
it through the existing `Store.Join`
([extension store](../../extensions/store.go)). A handler that writes slots and reads
them back in one transaction therefore sees its own uncommitted writes. Otherwise
fetch uses the manager's own read-only repeatable snapshot (`Store.Read`). Custom
transactor wrappers must supply the actual `*database.Tx` for read-your-writes;
an unrecognized executor uses the store snapshot, and the guide documents this.
The parent query and slot fetches remain separate statements, as for relations.

Fetches keep the managers' active-owner recheck. Removing it for parents that
were just read requires measured benefit and a review of concurrent deletion.

## Batching and cost

Each slot costs its manager's existing batch queries per parent batch,
independent of row count: an owner check plus keyset pages for translations, two
SELECTs plus one variant query for attachments, and two SELECTs for metadata.
Parent batches follow `RelationLimits.BatchSize` (500 by default, below the
managers' 1000-owner bound); attachment batches are further limited so the
collection's `MaxFiles` fits `MaxBatchFiles`. A batch whose rows or retained
bytes exceed a store's bound is halved and retried (`extensions.LoadInParts`),
so large content loads whenever each owner fits alone, without sizing every
batch for the worst case. Loading all translated slots of one model through a
single owner check and translation query remains an optional optimization that
requires a query-count test and a benchmark showing benefit.

## Reading loaded slots

All reads are pure. A slot that is not loaded reports that state or an error; it
never performs I/O.

- `translations.Text` exposes `Values() (translations.Values, bool)` and the
  existing `Exact`, `Resolve` and `Entries` semantics: empty text is present,
  unsupported locales fail and fallback follows `i18n.LocaleSet.Fallbacks`. A
  request helper resolves the locale from `i18n.RequestLocale(ctx)`, the existing
  typed request metadata, and otherwise uses the snapshot's default locale.
- `attachments.One[M]` exposes `Get() (value.Optional[attachments.File[M]], bool)`.
  `attachments.Many[M]` exposes `Get() ([]attachments.File[M], bool)`, `Len` and
  `All`, in collection order.
- Bound attachment descriptors derive links from loaded slots without database or
  storage I/O: `x.Logo.PublicURL(ctx, article)`, `x.Galleries.PublicURLs(ctx,
  article)`, and `URL`, `TemporaryURL` and `VariantURL` for one loaded file. They reuse the rules of
  `PublicURLOf`, `TemporaryURLOf` and `VariantPublicURLOf`, including active-content
  refusal and `VariantUnavailable`.
- `metadata.Value[V]` exposes `Get() (value.Optional[V], bool)`. Values are decoded
  once at load; stored-version mismatches and malformed values fail the load.

Localized attachment collections keep their explicit `ForLocale`/`LoadLocalized`
API in this series. A localized slot kind needs its own resolution design.

## Acceptance

- PostgreSQL statement counts: 1 and 100 articles with three slots issue the same
  statements, a forced two-batch read exactly two batches, and `LoadMissingFrom` on
  loaded slots none; 1000 articles are measured by the E05 benchmark. Nested
  relation plus slot; numbered, simple and cursor pagination; chunked iteration;
  `LoadFrom` and `LoadMissingFrom`.
- Not loaded, loaded empty and loaded present stay distinct for every slot kind.
- Soft-deleted owners, removed catalog locales, unready attachments and variants
  behave as their managers already define.
- Owner, row, byte and `MaxRows` bounds fail without partial results.
- Read-your-writes inside a transaction on the store's pool; store-snapshot
  behavior on another pool and through an unrecognized executor.
- Cancellation stops a slot load before fetching. Manager overload and admission
  release are the managers' existing contract, which slot loads reuse unchanged
  (`Store.Read`/`Join` and attachment read admission).
- An unbound slot fails before parent SQL. Compile-fail: another model's slot in
  `With`. Real gopls hover on slot read methods.

Finish all implementation, documentation and tests, then execute the common
milestone gate.
