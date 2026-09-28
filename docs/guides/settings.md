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
existing value and presentation. `Set` updates an existing value, returning
`database.NotFound` for a missing key. `Upsert` atomically creates or updates the
value and version, preserving existing presentation. `SetIn` and `UpsertIn` join
the caller's transaction through a savepoint; parent rollback includes settings.
No operation automatically retries an uncertain commit.

`Get` returns an omitted optional only for a missing row. `GetOr` uses its default
only in that case and validates/snapshots that default with the same codec.
Corrupt stored data, incompatible versions and invalid presentation return errors.
Writing a freshly validated value explicitly replaces its stored version; schema
upgrades require coordinated application deployment.

Presentation supports text, textarea, number, boolean, select, multiselect, email,
URL, color, date, datetime, file, image, JSON, password and code widgets.
`Configure` changes a persisted presentation explicitly. The widget does not
replace the value codec's validation. `Parameters` is an explicitly dynamic JSON
object of at most 16 KiB. The password widget masks presentation only; this table
does not encrypt values.

`Find` returns a record with presentation and timestamps. Records have no implicit
JSON serialization; decode through the typed key or deliberately use
`DynamicValue`. `List` selects only this manager's registered keys, ordered by
group, sort order and name. `Filter` supports a group, literal name prefix and
explicit `PublicOnly`; private settings are excluded when `PublicOnly` is true.
Callers still own endpoint authorization. The list uses one streaming query,
at most 1000 registered keys and a 4 MiB JSON budget, failing without partial
results when exceeded. `Groups` lists the groups in that bounded result.
`Remove` returns whether a row was deleted and is safe to repeat.
