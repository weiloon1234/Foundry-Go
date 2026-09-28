# Model extensions

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

See also
[attachments](attachments.md) for collection policy and recoverable storage.

## Shared owners

Declare an owner from the generated model query and primary field once:

```go
var Users = extensions.DefineOwner("users",
    query.IdentityOf(models.QueryUsers().Query, models.UserFields().ID))
```

`IdentityOf` requires bare generated model metadata and the actual primary field.
Different model/key types fail at compilation; wrong manually supplied fields,
scoped queries and sensitive keys fail validation. It retains the primary codec,
including natural keys, and the generated soft-delete policy. It selects stored
keys directly, without model presentation getters or retrieval observers.

Register each owner with `extensions.NewRegistry(Users.Registration(), ...)`,
then construct `extensions.New(database, registry, config)`. Reuse that exact
owner declaration for all extension descriptors. Duplicate names and duplicate
model namespaces are rejected. The store borrows the database and has no
automatic migration or seeding. Its schema must contain the framework tables
and the unqualified owner tables; explicitly qualified owner metadata can refer
to a separate schema. `extensions.Module` owns store shutdown when used through
the application container.

An owner reference captures identity only. Every operation checks existence from
the database. Soft-deleted owners are unavailable to ordinary extension reads and
writes, while their data remains available after restoration. The application
must still enforce its authorization policy before invoking an extension.

Store writes lock the active owner row before changing extension data. That lock
serializes simultaneous writes and conflicts with deletion or soft deletion.
Read batches use one read-only repeatable snapshot for the owner and value
queries. Stored subject keys use database equality semantics, including signed
floating zero and equivalent interval keys. A snapshot is not a later request's
authorization or eligibility cache.

## Typed metadata

```go
var Theme = metadata.Define(Users, "theme", 1, contract.StringJSON[string]())

manager, err := metadata.New(store, Theme.Registration())
err = Theme.Set(ctx, manager, user.FoundryReference(), "dark")
theme, err := Theme.Get(ctx, manager, user.FoundryReference())
```

Keys bind owner, key name, version and JSON value contract. String, boolean,
integer and floating descriptors are available from `contract`; structured
values use their generated DTO JSON descriptor. Native widths, exact integer
values, strict JSON syntax and canonical equality are shared with existing
framework contracts. `contract.DynamicJSON()` is the explicit unrestricted JSON
facility; it still enforces syntax and resource bounds.

Apply `metadata.Migrations()` with the ordinary migration runner in the shared
store schema. Metadata has ordinary generated framework models internally.
Public operations never ask applications to reproduce table or codec schemas.

`Get` returns an omitted optional for a missing key and `database.NotFound` for
a missing/soft-deleted owner. Malformed values and incompatible stored versions
return errors instead of silently coercing data or supplying a default. `Set`
is an explicit upsert and can replace an old version with a freshly validated
value; rolling version changes need application coordination. `SetIn` joins a
business transaction through an isolated savepoint and restores its search path.
Parent rollback rolls back the metadata write. There is no automatic transaction
retry after an uncertain commit.

`Theme.Load(ctx, manager, references)` loads one typed key for up to 1000 owner
references using two SELECTs: active keys and matching metadata. Repeated batch
access performs no I/O and returns freshly decoded values. Empty batches do not
query either table. Metadata accepts values up to 256 KiB and at most 256 keys per
owner. Streaming batch reads retain at most 4 MiB of JSON and fail without
returning partial results if that bound is exceeded. Oversized input fails
before persistence. Larger application datasets
should use ordinary typed model tables.

`metadata.All` returns the complete bounded set for an active owner. A record
retains model ownership and does not implicitly serialize its private value.
Decode it with its matching key or use `DynamicValue` explicitly for an
administrative export. `Forget` removes one key, while `DeleteAll` removes the
owner's metadata. `Theme.Matching(ctx, manager, value)` returns a typed membership
predicate for at most 1000 matching stored owners. It is a materialized scope;
the subsequent model query retains its own visibility and authorization filters.

## Cleanup and orphan inspection

Call `metadata.Cleanup` from the generated model's transactional Deleted or
ForceDeleted observer, passing its original typed reference and lifecycle
operation. It joins the same database transaction, refuses cleanup while the
owner still exists, and rolls back with the owner deletion. SoftDelete preserves
data. Normal bulk model deletes skip per-model observers, so explicit bulk
cleanup or subsequent orphan maintenance is required.

`metadata.InspectOrphans` scans bounded lexicographic pages for one registered
owner. Soft-deleted owners count as retained. The page size limits scanned rows,
not matches, so sparse orphan sets do not repeatedly starve later records.
`PruneOrphans` is a separate explicit operation: it locks exact selected rows,
rechecks retained ownership, and leaves foreign or retained rows unchanged.
Unknown owners and corrupted identity metadata fail closed.

`metadata/command.Parse` provides
`metadata orphans --owner users --page-size 100 --format text|json`. Parse before
booting services, then run the command with the assembled manager and output
writer. It inspects every page without deleting and prints only opaque row keys
and metadata names. JSON emits one page per line; it never exports model identity
or value payloads. Framework CLI composition remains in milestone 23.

The common store bounds operation count and owns actual callback lifetime. Close
cancels operations, the caller's context bounds waiting, and Done closes after
actual exit. Codecs and injected clocks must be deterministic, concurrency-safe,
and honor their documented context contracts. No process-global extension cache
or automatically inferred owner table is created.

## Translated model fields

```go
var ProductName = translations.Define(Products, "name", translations.Options{})

locales, err := i18n.NewLocaleSet("en", "en", "ms", "zh")
manager, err := translations.New(store, locales, ProductName.Registration())
err = ProductName.Set(ctx, manager, product.FoundryReference(), "ms", "Baju Merah")
name, err := ProductName.Resolve(ctx, manager, product.FoundryReference(), "ms")
```

`i18n.LocaleID` is a canonical BCP 47 tag. Normalize external text with
`i18n.ParseLocale`; canonical validity does not grant supported-locale membership.
The injected `LocaleCatalog` supplies one immutable supported/default snapshot
per operation. `LocaleSet` itself is a static catalog. The complete message and
request localization system remains in milestone 20; model content never uses a
process-global current locale.

`Get` returns the exact locale's optional text. `Resolve` checks the requested
locale, then the catalog default, then the first lexicographically ordered
supported translation. Empty text is a present value. Unsupported locales fail
instead of bypassing catalog membership through fallback. Locale removal retains
stored rows for migration but excludes them from ordinary field values.

`ProductName.Load(ctx, manager, references)` uses one active-owner query and one
streaming translation query in the same snapshot. `batch.Get(reference)` performs
no I/O and exposes immutable `Values`; `Entries` returns a fresh map. Limits are
1000 owner references, 4096 returned translation rows and 4 MiB of text per batch.
Each field defaults to 64 KiB per value and may declare a smaller `MaxBytes`.

`translations.Set(ctx, manager, reference, ProductName.SetValue(locale, text), ...)`
atomically writes multiple fields/locales for one typed owner. It validates every
assignment before writing; duplicates fail. `SetIn` joins a business transaction.
An owner can have 64 declared fields and 4096 persisted locale/field pairs.

`All` provides bounded administrative inspection, including removed locales and
old fields. `Forget` removes an exact active-locale value, `Clear` removes all
locales of a declared field, and `DeleteAll` clears the active owner's translations.
`Matching` supplies a typed, bounded exact-value owner scope. Authorization stays
with the caller and the subsequent model query.

`translations.Cleanup` has the same transactional hard-delete and soft-delete
semantics as metadata. `InspectOrphans` and explicit `PruneOrphans` share owner
validation and bounded pagination. Inspection selects only ownership/index
columns; it does not load private content. `translations/command` accepts
`translations orphans --owner products --page-size 100 --format text|json` and
exposes no deletion flag. Apply `translations.Migrations()` explicitly.

Settings and country reference data use the same store and are documented in
[Settings](settings.md) and [Countries](countries.md).
