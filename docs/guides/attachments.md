# Model attachments

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

Attachments share the registered [model extension owner](model-extensions.md#shared-owners),
ordinary generated infrastructure models, and explicit versioned migrations.
Apply `attachments.Migrations()` through the normal migration runner. The
constructor performs no migration, storage write, seeding or worker startup.

## Declare collection policy once

```go
var ProfileFiles = storage.DefineDisk("profile-files")
var Avatar = attachments.Define(Profiles, "avatar", attachments.Policy{
    Disk: ProfileFiles,
    Cardinality: attachments.Single,
    Accepted: []storage.MediaType{"image/png", "image/jpeg"},
    Image: value.Set(imaging.NewPlan().Fill(256, 256, true).Format(imaging.WebP)),
})

manager, err := attachments.New(attachments.Dependencies{
    Store: store, Disks: disks, Image: engine,
}, attachments.DefaultConfig(), Avatar.Registration())
result, err := Avatar.Replace(ctx, manager, profile.FoundryReference(), upload)
```

Reuse the exact owner, disk and collection descriptors in assembly and calls.
Redeclaring a matching name cannot replace a registered policy. Collection calls
retain the owner's model and primary key type; file IDs also retain the model.
See the [independent consumer](../../tests/fixtures/consumer/profiles/profile.go).

`Single` replaces the current file. `Multiple` appends up to `MaxFiles` (100 by
default, at most 256); `Replace` replaces the entire current collection. `Reorder`
requires an exact permutation with no duplicate, omitted or foreign ID.
`SetProperties` replaces explicit JSON object metadata, bounded to 64 KiB. It
cannot change storage identity, owner, media policy or collection membership.

For file collections, supply canonical exact `Accepted` media types or explicitly
choose `AnyMedia`. Acceptance is detected from bytes, independent of the filename
and caller's content-type hint. Detection alone is not a full content validator.
An `Image` plan requires bounded image inspection, pixel decoding and re-encoding;
its accepted media types apply to the input, and stored metadata describes the
output. Image policies without an accepted list allow the supported input codecs.
See [image formats, transforms and limits](imaging.md).

`Upload.Source` is borrowed until the call actually returns and is never closed.
Input and stored-output defaults are 8 MiB, with an absolute 64 MiB ceiling.
Image-engine limits also apply, and the tightest limit wins. Properties, reader
length, filename syntax and content are validated before a storage intent is
created. A filename is display metadata only; storage uses a unique immutable
framework key. It never becomes a filesystem path.

Before hooks run in declaration order with detected metadata and the typed
owner. AfterStored hooks run after storage acknowledgement inside the ownership
transaction; an error, panic or Goexit vetoes publication. Use transactional
outboxes for external effects from that transaction. The framework rechecks the
owner and acknowledged object identity after these hooks.

## Publication and recovery

The storage disk must implement conditional creation, reading and deletion.
The workflow commits a `writing` intent before `Put` with an absence condition.
Successful storage is pinned by ETag and optional version, then journaled as
`stored`. A separate owner-locked transaction publishes `ready` membership and
retires superseded files as `cleanup`. Old files are deleted only after that
transaction's commit is confirmed, using their exact validators/versions.

Always inspect `Result.Publication` when an upload returns an error:

| Result | Meaning |
| --- | --- |
| `Unpublished` | This operation has not established ready membership. The old collection is preserved. |
| `Published` | Ownership committed. `Attachment` is available even if a later hook or old-file cleanup failed. |
| `PublicationUnknown` | The ownership commit outcome is uncertain. Inspect the durable operation and fresh collection before deciding whether to retry. |

`PendingCleanup` contains durable operations whose object cleanup remains open.
`Manager.Inspect` reads an operation's current state, attempt count and safe
failure category. It does not expose provider errors or private upload metadata.
Cleanup failure never rolls back an already published replacement or deletes a
different current object at the same key. Confirmed absence makes retries
idempotent; a changed object remains pending for operator review. Error
inspection is bounded to 256 nodes and 64 unwrap levels per search. An incomplete
storage error search cannot prove absence; cleanup remains pending. An incomplete
transaction outcome search remains unknown. Custom error methods must return.

`Reconcile` handles known cleanup intents. `ReconcilePending(limit)` performs an
explicit bounded sweep of cleanup and abandoned stored intents. Stored intents
must exceed `StoredGrace` (five minutes by default), and locally active writers
are excluded. Moving stored to cleanup locks and fences any later publication
before deleting bytes. There is no hidden background worker.

An uncertain storage write is different from a known stored object. `writing`
and `uncertain` intents are never reclaimed merely because a deadline passed or
one Stat returned not-found: a remote write could still complete. After an
operator establishes that the writer stopped and the provider request settled,
`Settle(..., WriterStoppedAndStorageSettled, queue)` fences the writer, validates
the pinned object's complete size/checksum, and abandons it safely. An active
local writer cannot be settled. If publication cannot be determined, preserve
the intent for inspection instead of guessing.

## Ordinary jobs and outbox

```go
job := attachments.DefineReconcileJob("attachments.reconcile",
    jobs.DefaultPolicy("maintenance"))
declaration, err := job.Declare(manager)
// Assemble the ordinary job registry, dispatcher and durable outbox producer.
queue, err := job.ToOutbox(producer)
avatar := Avatar.WithReconciliation(queue)
result, err := avatar.Replace(ctx, manager, profile.FoundryReference(), upload)
```

Binding a queue does not change collection identity or policy. Cleanup and job
outbox rows commit in the same transaction, including owner lifecycle cleanup.
Normal synchronous cleanup still runs after commit; the queued job is an
idempotent recovery path. A rollback publishes neither row. Configure and run the
existing outbox publisher and worker explicitly. A periodic ordinary job can call
`ReconcilePending` to recover stored intents left by a crashed publisher.
Unknown writers require operator settlement; worker retries do not infer it.

## Reads and localization

`List`, `First`, `Find` and `Load` recheck current owner visibility. `Load` takes
up to 1000 owner references, uses one repeatable-read snapshot and two SELECTs
for a nonempty active set, and returns an in-memory batch. Batch access performs
no I/O and returns copied slices. The aggregate limit is 4096 files and 4 MiB of
properties. A missing or soft-deleted owner differs from an active empty
collection. Snapshot results do not replace fresh authorization checks.

`ReadBytes` rechecks ready membership, conditionally reads pinned storage, and
verifies the complete checksum and length. `Image` applies an explicit plan to
those bytes. `PublicURL` verifies that the current object still matches the pin;
`TemporaryURL` verifies the pinned version and delegates bounded signing to the
disk. A URL remains subject to the provider's capabilities and access policy.
Attachments reject implicit JSON serialization; map authorized data to a DTO.

Localized collections require `Localized: true` and an injected
`i18n.LocaleCatalog`. Use `ForLocale(id)` for exact writes and reads. No process
or request-global locale is inferred. Use the base declaration's `LoadLocalized`
for one catalog snapshot and one owner/file database snapshot. Resolve its
loaded values using requested locale, configured default, then remaining
supported locales in lexical order. Resolution chooses a complete nonempty
locale collection; it never mixes different locales' files. Removed catalog
locales remain stored but unavailable until supported again. Milestone 20 adds
message catalogs and request locale resolution to this same locale contract.

`Matching` provides a typed materialized owner query predicate for a collection
and exact locale. It is bounded to 4096 files/1000 distinct owners. The later
ordinary model query keeps its own visibility and authorization filters; the
predicate is a captured membership list, not a live subquery.

## Deletion and inspection

`Detach` and `Clear` retire ready ownership transactionally, then clean storage.
`DetachKeepFile` instead records durable `retained` state and returns explicit
storage pins. Cleanup responsibility passes to the application. Owner deletion,
orphan pruning and reconciliation preserve that object. `RetainedObject` recovers
its pins after an uncertain detach commit. Retained state is distinct from
cleaned; no file deletion is implied.

Call `attachments.Cleanup` from the generated owner's Deleted observer with the
actual transaction and lifecycle operation. Soft deletion preserves files;
restoration makes them visible again. Hard-delete cleanup first proves that the
owner is absent even with trashed rows, joins a savepoint, retires ready/stored
rows, and registers after-commit cleanup. A parent rollback restores ownership
and suppresses storage deletion. Unsettled writes remain visible for explicit
settlement. Bulk model deletion that skips observers requires maintenance.

`InspectOrphans` pages over ownership records, including soft-deleted owner
checks. `PruneOrphans` accepts exact inspected operation IDs, rechecks owners
under the transaction, and refuses unsettled writers. The read-only
`attachments/command` adapter accepts `attachments orphans --owner profiles
--page-size 100 --format json`; it has no delete flag. It can be registered in
the command kernel supplied by milestone 23.

`InspectStorage` lists one bounded provider page under the selected registered
owner's framework prefix and a registered collection disk. Its opaque cursor
retains the first-page age cutoff and disk/owner policy. Every journal state
protects its key. Untracked old objects are reported as candidates only; this
listing is not a transactional snapshot and does not authorize deletion.

The manager borrows the extension store, disks, image engine, locale catalog and
optional outbox producer. Stop workers first; then close the manager and await
`Done` before closing those dependencies. Admission defaults to two active calls,
with a two-minute operation deadline and ten-second cleanup budget. Cancellation
does not abandon a borrowed reader, codec or provider callback. `Close(ctx)`
bounds its caller's wait while actual work retains capacity; `attachments.Module`
drains it before completing dependency shutdown.
