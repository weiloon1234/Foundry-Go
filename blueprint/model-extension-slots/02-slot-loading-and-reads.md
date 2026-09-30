# E02 — Slot loading and reads

Prerequisites: E01. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Existing behavior and additions

`query.With` accepts `query.Relation[M]`, which is sealed: its methods are
unexported and its loader receives only a `database.Executor`
([relation loading](../../database/query/relation_loading.go)). Aggregates already
use a relation whose fetch function is supplied by a constructor and whose slot
getter/setter is bound by generated code
([aggregate relation](../../database/query/relation_aggregate.go)). The extension
managers already batch-load one declaration for up to 1000 owners inside one
read snapshot ([translations](../../translations/read.go),
[attachments](../../attachments/read.go), [metadata](../../metadata/values.go)).

E02 connects the two. A bound slot descriptor becomes an ordinary eager-loading
relation, so extension data loads wherever relations load.

## Externally fetched slot relation

Add one exported relation kind to `database/query` with the aggregate shape. The
extension package supplies its fetch function; generated slot bindings supply the
slot name, model table, getter and setter:

```go
type SlotFetch[M, S any] func(ctx context.Context, executor database.Executor, parents []M) ([]S, error)

type ExtensionBinding[M, S any] struct {
    Name, Table string
    Get         func(M) S
    Set         func(M, S) M
    Loaded      func(S) bool
    Count       func(S) int
    Fetch       SlotFetch[M, S] // nil leaves the slot unbound
    Unbound     error
}

func NewExtensionSlot[M, S any](binding ExtensionBinding[M, S]) ExtensionSlot[M]
```

Slot descriptors embed `query.ExtensionSlot[M]`, which implements the sealed
relation interface, so a bound descriptor passes directly to `With`.
Fetch returns one loaded slot value per selected parent, index-aligned, as
aggregate fetches do. `database/query` imports no extension package. The relation:

- validates before parent SQL; an unbound descriptor fails with a fault naming the
  slot and `<Model>Extensions().From(runtime)`;
- skips already loaded slots under `LoadMissing` through `IsLoaded`;
- charges each loaded value (a stored text set, metadata value or file) against
  the shared `RelationLimits` budget as fetched and attached, per parent batch, so
  an over-budget read stops early without publishing partial parents;
- enforces `MaxDepth` like other relations;
- honors cancellation and discards the complete result on any fetch error.

`TextSlot`, `OneSlot`, `ManySlot` and `ValueSlot` descriptors embed it.
`(<Model>ExtensionSlots).All()` returns every bound slot for an edit screen.

## Where slots load

Anywhere existing relations load: `All`, `First`, `Find`, `RequireFind`, numbered,
simple and cursor pagination, `Each`/`Chunk` per batch, explicit `Load` and
`LoadMissing`, and nested inside relation descriptors
(`QueryUsers().With(UserRelations().Articles.With(x.Title))`) and pivot models.
`Count` and `Exists` never load slots. Repeating one slot in a query fails
validation through the existing duplicate-slot check.

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
  statements, a forced two-batch read exactly two batches, and `LoadMissing` on
  loaded slots none; 1000 articles are measured by the E05 benchmark. Nested
  relation plus slot; numbered, simple and cursor pagination; chunked iteration;
  `Load` and `LoadMissing`.
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
