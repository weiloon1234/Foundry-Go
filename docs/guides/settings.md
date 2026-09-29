# Settings

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

These settings are application data, with explicit typed declarations
and migrations. They are separate from deployment configuration and secrets.

```go
var SiteName = settings.Define("site.name", 1, contract.StringJSON[string](),
    settings.Presentation{Label: "Site name", Group: "site", Public: true})

manager, err := settings.New(store, SiteName.Registration())
err = SiteName.Ensure(ctx, manager, "My site")
name, err := SiteName.GetOr(ctx, manager, "Unnamed site")
```

Use the same declared key in registration and calls. Its name, version and JSON
codec cannot be replaced by a lookalike declaration. Structured settings use a
generated DTO's JSON contract; primitive helpers live in `contract`.
`contract.DynamicJSON()` is the explicit unrestricted JSON option. Values are
snapshotted canonically and decoded afresh, preserving integer precision and
avoiding shared mutable maps/slices. A value is limited to 256 KiB.

Apply `settings.Migrations()` through the ordinary migration runner in the
[extension store](model-extensions.md#shared-owners)'s schema. The manager borrows
that store and its bounded operation/shutdown lifetime. It creates no background
goroutines or database resources of its own.

`Create` fails if the key exists. `Ensure` creates it if absent and preserves an
existing value and version. `Set` updates an existing value, returning
`database.NotFound` for a missing key. `Upsert` atomically creates or updates the
value and version, preserving existing presentation. `SetIn` and `UpsertIn` join
the caller's transaction through a savepoint; parent rollback includes settings.
No operation automatically retries an uncertain commit.

`Get` returns an omitted optional only for a missing row. `GetOr` uses its default
only in that case and validates/snapshots that default with the same codec.
Corrupt stored data, incompatible versions and invalid presentation return errors.

## Cached reads

Hot settings can opt into a per-process read cache owned by the manager:

```go
manager, err := settings.New(store,
    SiteName.RegistrationWith(settings.Options[string]{Cache: 30 * time.Second}))
```

`Get`, `GetOr`, `Find` and `Load` then serve a validated snapshot without a
database query until the entry is older than `Cache` (at most `MaxCacheTTL`, one
hour), and decode a fresh value on every call. Every write through the same
manager (`Create`, `Ensure`, `Set`, `Upsert`, `Configure`, `ResetPresentation`,
`Remove`, `Reconcile`) invalidates the entry before and after the write; joined
writes (`SetIn`, `UpsertIn`) invalidate again after the caller's transaction
commits, so a concurrent read cannot keep the old value. Other processes, and
changes made outside the manager, become visible after at most `Cache`: that is
the staleness bound for multi-process deployments. `manager.Invalidate(keys...)`
drops entries explicitly, for example after a SQL migration. Keys without
`Cache` read the database each time. The cache is bounded by the registered keys
and holds no process-global state. The cache uses the extension store's clock.

## Versions and upgrades

Increment a key's version when its stored JSON changes shape, and declare how
older values convert:

```go
var MailPort = settings.Define("mail.port", 2, contract.IntegerJSON[int64](),
    settings.Presentation{Kind: settings.Number, Group: "mail"})

registration := MailPort.RegistrationWith(settings.Options[int64]{Upgrades: []settings.Upgrade[int64]{
    settings.UpgradeFrom(1, contract.StringJSON[string](), func(_ context.Context, old string) (int64, error) {
        return strconv.ParseInt(old, 10, 64)
    }),
}})
```

The previous codec decodes the stored value; the result is re-encoded and
validated by the current codec. Reads convert an older row in memory, so a
deployment keeps working before the conversion is persisted. A stored version
with no declared upgrade, or newer than the declaration (a rolled-back
deployment), makes reads fail with `fault.Conflict`.

`settings.Reconcile(ctx, manager)` makes that an explicit startup step instead of
a failure on every later read. Run it after migrations when starting serve and
worker processes, and treat an error as a boot failure. In one transaction it
persists declared upgrades and refreshes declaration-owned presentations. If any
row is incompatible it writes nothing, returns `fault.Conflict`, and lists the
setting names in `Reconciliation.Incompatible`. It reads presentation columns
first and loads one value at a time for upgrades. Upgrade functions are
application code: keep them deterministic and free of I/O; panics are contained.

## Presentation

Presentation supports text, textarea, number, boolean, select, multiselect, email,
URL, color, date, datetime, file, image, JSON, password and code widgets. The
widget does not replace the value codec's validation. `Parameters` is an
explicitly dynamic JSON object of at most 16 KiB. The password widget masks
presentation only; this table does not encrypt values.

A presentation written from a declaration follows later declaration changes:
`Ensure` and `Reconcile` refresh it without touching the value. `Configure`
explicitly takes ownership of the persisted presentation, which declarations then
no longer replace (`Record.Configured()` reports this); `ResetPresentation`
restores the declared presentation and hands ownership back. Rows created before
the `000002_add_presentation_ownership` migration are treated as configured and
keep their stored presentation until `ResetPresentation` is called.

## Listing and batches

`Find` returns a record with presentation and timestamps. Records have no implicit
JSON serialization; decode through the typed key or deliberately use
`DynamicValue`. Listing selects only this manager's registered keys (at most
1000), ordered by group, sort order and name. `Filter` supports a stored group, a
literal name prefix and explicit `PublicOnly`; private settings are excluded when
`PublicOnly` is true. Callers still own endpoint authorization.

`ListPage(ctx, manager, filter, cursor, limit)` returns one bounded page
(`DefaultPageSize` 100, at most 1000) after a `Cursor` of group, sort order and
name; continue with `Page.Next`, which is zero after the last page, or build a
cursor from a record with `settings.After`. A page ends early rather than retain
more than 4 MiB of JSON, so large settings never make a listing fail. `List`
reads consecutive pages (each its own snapshot) and fails only above
`MaxLoadBytes` (64 MiB); use `ListPage` for larger administrative listings.
`Groups` reads presentation columns only and never loads values.

`settings.Load(ctx, manager, SiteName, MailPort)` reads several registered keys
with at most one query, serving cached keys from the cache.
`settings.LoadGroup(ctx, manager, "mail")` loads every key whose *declared* group
is `mail`, including keys without a stored row. Decode with
`MailPort.From(ctx, batch)` or `FromOr(ctx, batch, fallback)`; neither performs
I/O, and reading a key that was not selected is an error. A batch retains at most
`MaxLoadBytes` of JSON. `Remove` returns whether a row was deleted and is safe to
repeat.
