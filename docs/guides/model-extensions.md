# Model extensions

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

See also
[attachments](attachments.md) for collection policy and recoverable storage, and
[model extension slots](model-extension-slots.md) for declaring translated text,
attachments and metadata as typed fields on the model itself.

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

The store admits 64 concurrent operations by default (`MaxActive`, at most 1024),
each bounded by `Timeout` (one minute). A burst beyond capacity, for example
while the database is slow, queues in FIFO order for up to five seconds (or the
caller's shorter deadline) instead of failing immediately; only an unsatisfied
wait reports `fault.Overloaded`, which HTTP maps to a retryable 503.

### Owner identity and table renames

Every stored metadata row, translation, attachment and registration key belongs
to the owner's scope, an opaque digest of the owner name and its *storage model*.
The storage model defaults to the generated table name, so renaming an owner
table silently moves the owner to a new, empty scope unless its persisted
identity is declared. Declare the previous table name before renaming:

```go
var Users = extensions.DefineOwnerWith("users",
    query.IdentityOf(models.QueryMembers().Query, models.MemberFields().ID),
    extensions.OwnerOptions{StorageModel: "users"})
```

The scope, row keys and attachment object paths are then unchanged, and stored
identities attributed to the old table decode through the current key codec. New
applications may declare a stable `StorageModel` from the start. A storage model
cannot name another registered owner's table or storage model. Keep it unchanged
for the lifetime of the stored data.

If a table was already renamed without a declaration, rows written under the old
scope are hidden but intact. Declare the old table name as a previous model:

```go
var Users = extensions.DefineOwnerWith("users",
    query.IdentityOf(models.QueryMembers().Query, models.MemberFields().ID),
    extensions.OwnerOptions{PreviousModels: []string{"users"}})
```

`metadata rescope --owner users` and `translations rescope --owner users` (or
`metadata.InspectStale`/`Rescope` and the translation equivalents) list rows
whose owner column matches but whose scope differs from the owner's current
scope; `--apply` moves them in bounded, locked pages after verifying each row's
key digest and re-decoding its key with the current codec. Only rows whose
recorded scope and identity both belong to a declared model (the current model,
`StorageModel` or `PreviousModels`) are adopted; rows of any other earlier model
are reported as `undeclared`. A row whose subject no longer exists is reported
as `missing` instead of becoming an orphan in the current scope, and a row whose
current-scope equivalent already exists is never overwritten; it stays and is
reported as a `conflict` for an explicit decision. Previous models, like storage
models, cannot name another registered owner's model.
Attachments cannot be re-scoped because object paths embed the scope, so declare
`StorageModel` before renaming a table that owns attachments.

### Natural keys and bulk deletes

Extension data belongs to the owner's key, not to a row instance. Ordinary
model deletes clean it up through the `Cleanup` observers below, but set-based
bulk deletes skip per-model observers. For natural-key owners (country codes,
slugs, SKUs) a row recreated with the same key after such a delete therefore
inherits the old metadata and translations. After a bulk delete, remove the
extension data before recreating keys: call `DeleteAll` for each affected owner
inside the same transaction, or run orphan inspection and `PruneOrphans` before
the keys are reused. Soft deletion deliberately retains data for restoration.

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
`metadata orphans --owner users --page-size 100 --format text|json`, and
`metadata undeclared --owner users` lists stored key names no registered key
declares (see [slot maintenance](model-extension-slots.md#inspection-and-maintenance)). Parse before
booting services, then run the command with the assembled manager and output
writer. It inspects every page without deleting and prints only opaque row keys
and metadata names. JSON emits one page per line; it never exports model identity
or value payloads. The same parser accepts
`metadata rescope --owner users [--page-size 100] [--format text|json] [--apply]`
for [stale scopes](#owner-identity-and-table-renames); without `--apply` it only
lists stale and target row keys, and rows left in place carry a `conflict`,
`missing` or `undeclared` marker. Framework CLI composition remains in milestone 23.

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
locale, then its supported regional parents (`en-GB` → `en`), then the catalog
default, then the first lexicographically ordered supported translation. UI
catalogs share the same `LocaleSet.Match` parent rule. Empty text is a present
value. Unsupported locales fail instead of bypassing catalog membership through
fallback. Locale removal retains stored rows for migration but excludes them from
ordinary field values.

`ProductName.Load(ctx, manager, references)` uses one active-owner query and
keyset-paged translation queries of 4096 rows in the same snapshot, so 1000 owners
in several locales load completely; most batches need a single page. `batch.Get(reference)`
performs no I/O and exposes immutable `Values`; `Entries` returns a fresh map.
Limits are 1000 owner references and 4 MiB of text per batch; rows are bounded by
owners × supported locales. Each field defaults to 64 KiB per value and may declare
a smaller `MaxBytes`.

`translations.Set(ctx, manager, reference, ProductName.SetValue(locale, text), ...)`
atomically writes multiple fields/locales for one typed owner with one set-based
`INSERT ... ON CONFLICT`. It validates every assignment before writing; duplicates
fail. `SetIn` joins a business transaction. An owner can have 64 declared fields
and 4096 persisted locale/field pairs.

`All` provides bounded administrative inspection, including removed locales and
old fields. `Forget` removes an exact active-locale value, `Clear` removes all
locales of a declared field, and `DeleteAll` clears the active owner's
translations; clearing and cleanup use one set-based `DELETE` without loading
text. `Matching` supplies a typed, bounded exact-value owner scope and selects only
ownership columns. The `000002_index_translation_values` migration adds a hash
index on the value, which serves exact equality for values up to 64 KiB; it is a
regular transactional `CREATE INDEX`, so apply it in a maintenance window on a
large existing table. Authorization stays with the caller and the subsequent
model query.

`translations.Cleanup` has the same transactional hard-delete and soft-delete
semantics as metadata. `InspectOrphans` and explicit `PruneOrphans` share owner
validation and bounded pagination. Inspection selects only ownership/index
columns; it does not load private content. `translations/command` accepts
`translations orphans --owner products --page-size 100 --format text|json` and
exposes no deletion flag, `translations undeclared --owner products` for stored
field names no registered field declares, plus `translations rescope --owner
products [--apply]` with the metadata re-scope semantics. Apply `translations.Migrations()` explicitly.

Settings and country reference data use the same store and are documented in
[Settings](settings.md) and [Countries](countries.md).
