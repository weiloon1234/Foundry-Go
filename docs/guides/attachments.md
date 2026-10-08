# Model attachments

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

A model can also declare collections as `attachments.One`/`Many` fields; see
[model extension slots](model-extension-slots.md). Slots reuse this policy and
publication workflow.

Attachments share the registered [model extension owner](model-extensions.md#shared-owners),
ordinary generated infrastructure models, and explicit versioned migrations.
Apply `attachments.Migrations()` through the normal migration runner. The
constructor performs no migration, storage write, seeding or worker startup.

AVIF uploads can use the same image policies and variants with
`Accepted: []storage.MediaType{"image/avif"}`. The portable engine handles
still AVIF and explicit animation plans without an OS codec installation.

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
and caller's content-type hint. Beyond the standard sniffing table, detection
recognizes OOXML documents (docx/xlsx/pptx and their macro-enabled forms, from
the ZIP part names), ODF and EPUB packages (their stored `mimetype` entry), SVG,
and ISO media brands (AVIF, HEIC/HEIF, MP4, QuickTime, 3GPP, M4A); other ZIP
archives stay `application/zip`. The caller's content-type hint can only
specialize generic plain text to CSV, TSV, Markdown, calendar or valid JSON, and
only when the policy accepts that type; otherwise the file stays `text/plain`.
Detection alone is not a full content validator.

> **Warning: script-capable files.** SVG (`image/svg+xml`), HTML, XHTML, other
> XML types and JavaScript can run scripts when a browser renders them inline,
> in the origin that serves them (stored XSS). Accepting `image/svg+xml` is not
> the same as accepting a raster image. By default attachments never link such
> files inline: `PublicURL`/`PublicURLOf` fail with `ActiveContentRefused`
> (matches `fault.Invalid`), and signed links force `Content-Disposition:
> attachment` (an explicit inline disposition is refused). Set
> `Policy.InlineActiveContent` only when those files are served from an isolated,
> cookie-less origin. Serving `Open` streams yourself carries the same risk:
> send them as downloads with `X-Content-Type-Options: nosniff`, or convert them
> to a raster format with an image policy.
An `Image` plan requires bounded image inspection, pixel decoding and re-encoding;
its accepted media types apply to the input, and stored metadata describes the
output. Image policies without an accepted list allow the supported input codecs.
See [image formats, transforms and limits](imaging.md).

`Upload.Source` is borrowed until the call actually returns and is never closed.
Input and stored-output defaults are 8 MiB, with an absolute 64 MiB ceiling.
Image-engine limits also apply, and the tightest limit wins. Properties, reader
length, filename syntax and content are validated before a storage intent is
created. A filename is display metadata only; storage uses a unique immutable
framework key. It never becomes a filesystem path. Zero-width and bidirectional
formatting characters (U+200B–U+200F, U+202A–U+202E, U+2060–U+2064,
U+2066–U+2069, U+061C, U+FEFF) are removed from the stored display name, so a
name such as `invoice<RLO>fdp.exe` cannot render as `invoiceexe.pdf`.

`AddFromURL(ctx, manager, owner, attachments.RemoteSource{Client, URL,
OriginalName})` imports a remote file for untrusted URLs. `Client` must enforce a
restricted destination policy (see [restricting destinations](http-client.md#restricting-destinations-from-untrusted-input));
an unrestricted client is rejected as invalid. The file is downloaded within the
collection's `MaxBytes` inside the manager's `MaxActive` admission, which covers
its download, validation, storage, publication and follow-up work. The manager
timeout and shutdown cancel admitted downloads; publication cleanup retains its
separate `CleanupTimeout`. One bounded input buffer is reused for validation and
storage; image processing has its own engine limits. A queued import sends no
request until admitted. Acceptance is detected from the bytes and the response
Content-Type is only a hint. Redirects, non-2xx responses, truncated and oversized
bodies fail without adding a file. `OriginalName` defaults to the URL's last path
segment.

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

To publish inside a caller's transaction, split the workflow in two.
`Prepare` runs the intent, storage and pin steps in the manager's own
transactions before that transaction begins and returns a `Prepared` upload; it
locks an existing owner while recording the intent, so the per-owner bound stays
exact, and accepts a model the transaction will create, with its key chosen
first. `ReplaceIn` and `AddIn` then publish it inside the caller's transaction
of the extension store's pool, joined through a savepoint, using only that
transaction's connection and at any isolation level, since the upload was
committed before its snapshot. They lock the owner, recount its intents and
publish; the attachment is part of the caller's commit. A failed or rolled-back
publication leaves the upload `stored`, ready for another attempt; `Discard`
reclaims it at once, and `ReconcilePending` cleans it after `StoredGrace`. Old-file
cleanup and unqueued variant generation run after the commit, so a failure there
is the transaction's after-commit error and leaves `cleanup` intents for
reconciliation. `Publication` is `Published` once the savepoint succeeds, subject
to that commit. A
retained version ID is deleted by version alone (it already selects one
immutable object, and providers such as AWS cannot combine a delete condition
with a version); without a version the ETag condition preserves a concurrent
replacement. Versioned buckets therefore clean up replaced and detached files
exactly, without leaving delete markers.

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

`ReconcilePending` also settles such intents automatically, but only once they
are older than the longest registered disk `Timeout` (after which the writer's
request context has ended) plus `Config.SettleAfter` (15 minutes by default, the
bound for a provider to finish a request it already received). It fences the
writer, then settles only unambiguous storage: an absent object, or a present
object whose size and full SHA-256 (provider checksum metadata, otherwise the
pinned bytes) exactly match the intent. Anything else stays `uncertain` with the
failure category `settlement_ambiguous` for an operator; it is never deleted.
Locally active writers are excluded.

## Image variants

```go
var Thumbnail = attachments.DefineVariant("thumbnail",
    imaging.NewPlan().Fit(320, 320, false).Format(imaging.WebP))
var Photos = attachments.Define(Profiles, "photos", attachments.Policy{
    Disk: ProfileFiles, Cardinality: attachments.Multiple,
    Accepted: []storage.MediaType{"image/jpeg", "image/png"},
    Variants: []attachments.Variant{Thumbnail},
})
url, err := Photos.VariantPublicURLOf(ctx, manager, file, Thumbnail)
```

A `Variant` is a named derived image built from each ready original with an
imaging plan and stored beside it; the original is never changed. Declare up to
`MaxVariants` (8) per collection, with unique semantic names. Variants require
image input: an `Image` policy or an `Accepted` list of decodable image types
(`AnyMedia` is rejected), and the manager requires an image engine.

Without a queue, missing variants are generated after the upload's publication
commits, inside the same admission slot. A generation failure never
unpublishes the original: the upload returns the error with `Published` and
`PendingVariants` set. `WithVariantQueue` binds a typed job instead
(`DefineVariantJob`, `Declare(manager)`, `ToOutbox(producer)`); the publication
transaction then enqueues one job and the result reports `PendingVariants`. The
job calls `Manager.GenerateVariants`, which is idempotent: it generates only
variants without a ready file.

Each variant has its own journal row (`foundry_attachment_variants`, migration
`000002_create_variants`) and object key under `foundry-attachment-variants/`.
It is written with an absence condition and becomes ready only while its
original is still ready, replacing the previous ready variant of that name.
Variants follow the original: replace, detach, clear, owner deletion and orphan
pruning delete them before the original, and the original stays `cleanup` (and
is retried) until every variant is confirmed deleted. `DetachKeepFile` hands
over only the original; its variants are removed. `ReconcilePending` retries
variant cleanup and settles aged `writing`/`uncertain` variant intents with the
same cutoff as originals; because an unresolved variant is never ready, a
present object is deleted rather than published. Storage inspection of
originals does not list the variant prefix.

Loaded attachments (`Load`, `List`, `First`, `Find`, `LoadLocalized`) carry their
ready variants from one extra bounded query. `Attachment.Variant(v)` and
`Variants()` report them. `VariantPublicURLOf` and `VariantTemporaryURLOf` derive
links with no database or storage I/O, pinned to the variant's stored version;
an undeclared variant is invalid and one not generated yet fails with
`VariantUnavailable` (it matches `fault.Missing`), so callers can fall back to the
original.

`Collection.RegenerateVariants(ctx, manager, RegenerationOptions{Limit: 50})`
processes one batch of ready attachments in ID order under one write slot and
returns `Next` until the collection is exhausted. By default every variant is
regenerated and replaces the previous one (for example after changing a plan);
`Missing: true` only fills gaps. Failed attachments are listed and keep their
previous variants. The `attachment-variants` command (`command.VariantDeclaration`)
runs the same batches: `attachments variants --owner profiles --collection photos
[--missing] [--batch 20] [--format json]`. It reports counts and failed operation
IDs only, and exits with an error when any attachment failed.

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
verifies the complete checksum and length (a disk-verified full read whose
stored checksum equals the pin is not hashed again). `Image` applies an explicit
plan to those bytes. `Open` streams an attachment of any size up to the policy
bound, pinned to its ETag/version and verified at EOF; only the lookup uses
manager read admission, and the reader holds one disk stream slot until closed.

`PublicURL` and `TemporaryURL` recheck ready membership and derive the link from
the stored key and version; keys are immutable per upload, so no storage request
is made. For attachments already loaded through `Load`, `List`, `First` or
`Find`, `PublicURLOf`, `PublicURLsOf` (in order, for listings) and
`TemporaryURLOf` derive links with no database or storage I/O. Links to script-capable
files follow the download-only default above. They accept only
attachments of the same collection, locale and disk; `TemporaryURLOf` always
pins the stored version and accepts signed response overrides such as a download
`ResponseContentDisposition`. Authorize access before creating any link. A URL
remains subject to the provider's capabilities and access policy.
Attachments reject implicit JSON serialization; map authorized data to a DTO.
For email, pass an authorized file, owned attachment or loaded single-file model
slot to `message.AttachStored(...)`. Its `EmailAttachment()` method also returns
a pinned `email.Attachment` for a typed queue payload. The framework resolves the
bytes through the configured disk; consumers do not convert them to blobs.
See [email attachment sources](email.md#storage-attachments-and-limits) for
browser uploads, lifetime requirements and provider-independent examples.

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
--page-size 100 --format json` and `attachments undeclared --owner profiles` for
stored collection names no registered collection declares; it has no delete flag. It can be registered in
the command kernel supplied by milestone 23.

`InspectStorage` lists one bounded provider page under the selected registered
owner's framework prefix and a registered collection disk. Its opaque cursor
retains the first-page age cutoff and disk/owner policy. Every journal state
protects its key. Untracked old objects are reported as candidates only; this
listing is not a transactional snapshot and does not authorize deletion.

The manager borrows the extension store, disks, image engine, locale catalog and
optional outbox producer. Stop workers first; then close the manager and await
`Done` before closing those dependencies. Uploads and other mutations use
`MaxActive` (default 8); loads, lookups, reads, inspection and link signing use a
separate `MaxReads` pool (default 64), so a burst of one kind cannot starve the
other. Both queue in FIFO order for at most `min(Timeout, 5s)` and then fail as
retryable overload (`fault.Overloaded`). Owner-delete observers and their
after-commit cleanup use their own bounded pool (`MaxOwnerCleanup`), so a model
deletion never fails because application uploads are busy. Operations have a
two-minute deadline and a ten-second cleanup budget. Cancellation
does not abandon a borrowed reader, codec or provider callback. `Close(ctx)`
cancels all three pools together and bounds its caller's wait while actual work
retains capacity; `attachments.Module` drains it before completing dependency
shutdown.

Native image formats use the same configured imaging engine as direct processing.
With the [libvips backend](imaging.md#automatically-discovered-libvips-runtime), collections can
accept HEIF/HEIC, JPEG 2000, JPEG XL or SVG and produce ordinary named raster
variants. Image inspection uses the engine's capabilities, including when an
original is retained and only variants are transformed. HEIC/HEIF media aliases
share acceptance rules. Backend availability and plan compatibility are checked
when constructing the attachment manager; no codec import is needed in consumers.
