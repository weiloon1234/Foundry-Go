# Model attachments

<a id="agent-entry"></a>
## Agent entry

- [Declare a collection](#recipe-declare-collection)
- [Publish and reconcile files](#recipe-publish-files)
- [Read current membership](#recipe-read-files)
- [Retire membership](#recipe-retire-files)
- [Declare model slots](model-extension-slots.md#recipe-declare-slots)
- [Validate uploads](http-uploads.md#recipe-file-kinds)
- [Serve private bytes](storage.md#recipe-storage-http)

## Overview

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

A model can also declare collections as `attachments.One`/`Many` fields; see
[model extension slots](model-extension-slots.md). Slots reuse this policy and
publication workflow.

Attachments share the registered [model extension owner](model-extensions.md#shared-owners),
handwritten ORM infrastructure declarations, and explicit versioned migrations.
Apply `attachments.Migrations()` through the normal migration runner. The
constructor performs no migration, storage write, seeding or worker startup.

AVIF uploads can use the same image policies and variants with
`Accepted: []storage.MediaType{"image/avif"}`. The portable engine handles
still AVIF and explicit animation plans without an OS codec installation.

<a id="recipe-declare-collection"></a>
## Declare collection policy once

<!-- compiled-source: tests/fixtures/consumer/profiles/profile.go#attachments-avatar -->
```go
var Avatar = attachments.Define(Owners, "avatar", attachments.Policy{Disk: Files, Cardinality: attachments.Single, Accepted: []storage.MediaType{"image/png", "image/jpeg"}, Image: value.Set(imaging.NewPlan().Fill(32, 32, false).Format(imaging.PNG))})
```

<!-- compiled-source: tests/fixtures/consumer/profiles/profile.go#attachments-example-2 -->
```go
func (s Services) ReplaceAvatar(ctx context.Context, profile Profile, upload attachments.Upload) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Avatar.Replace(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), upload)
}
```

<!-- compiled-source: tests/fixtures/consumer/profiles/attachment_recipes.go#add-collection-file -->
```go
func (s Services) AddDocument(ctx context.Context, profile Profile, upload attachments.Upload) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.Add(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), upload)
}
```

Reuse the exact owner, disk and collection descriptors in assembly and calls.
Redeclaring a matching name cannot replace a registered policy. Collection calls
retain the owner's model and primary key type; file IDs also retain the model.
See the [independent consumer](../../tests/fixtures/consumer/profiles/profile.go).

Bound slots accept request-owned uploads. Return the typed publication report
together with any error to the application decision boundary:

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#publish-file-sources -->
```go
type MediaPublication struct {
	Logo    value.Optional[attachments.Result[Article, model.ID[Article]]]
	Gallery []attachments.Result[Article, model.ID[Article]]
}

func (s *Service) PublishMedia(ctx context.Context, article Article, input ArticleMedia) (MediaPublication, error) {
	var publication MediaPublication
	if logo, supplied := input.Logo.Get(); supplied {
		result, err := s.x.Logo.ReplaceFile(ctx, article, logo)
		publication.Logo = value.Set(result)
		if err != nil {
			return publication, err
		}
	}
	results, err := attachments.AddFilesTo(ctx, s.x.Galleries, article, input.Galleries)
	publication.Gallery = results
	return publication, err
}
```

The optional logo result distinguishes an unattempted logo from a failed attempt.
Every returned result retains `Operation`, `Publication`, `PendingCleanup` and
`PendingVariants`. On a batch error, inspect successful earlier entries and the
failed entry; do not replay the whole batch. The HTTP consumer preserves this
report in `MediaPublicationError` for application error handling. Disclosure,
reconciliation and retry decisions remain application-owned.

`attachments.AddFilesTo` validates the bound handle and batch limit before opening
files, publishes in order, and returns every attempted result, including a failed
attempt. Earlier files remain published. Inspect unknown outcomes before retrying.

For one additional gallery file, use the multiple-slot handle directly. Prepared
files must come from that same handle before the transaction begins:

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#append-slot-file -->
```go
func (s *Service) AddGalleryFile(ctx context.Context, article Article, file attachments.FileSource) (attachments.Result[Article, model.ID[Article]], error) {
	return s.x.Galleries.AddFile(ctx, article, file)
}

func (s *Service) AddPreparedGallery(ctx context.Context, tx *database.Tx, article Article, prepared attachments.Prepared[Article, model.ID[Article]]) (attachments.Result[Article, model.ID[Article]], error) {
	return s.x.Galleries.AddIn(ctx, tx, article, prepared)
}
```

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

<a id="recipe-publish-files"></a>
## Publication and recovery

For a bound single slot, prepare before opening the transaction. This service
keeps the caller's visibility query for the locked owner read and model write:

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#publish-slot-file -->
```go
func (s *Service) ReplaceLogo(ctx context.Context, visible orm.Query[Article], article Article, file attachments.FileSource, slug string) (attachments.Result[Article, model.ID[Article]], error) {
	prepared, err := s.x.Logo.PrepareFile(ctx, article, file)
	result := attachments.Result[Article, model.ID[Article]]{Operation: prepared.Operation()}
	if err != nil {
		return result, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		locked, err := orm.RequireLocked(ctx, tx, orm.Lock(visible.Where(orm.KeyEq(Articles.Key(), article.ID))))
		if err != nil {
			return err
		}
		if _, err := orm.Update(ctx, tx, visible, Articles.Key(), locked.ID, orm.TextSet(Articles.Slug, slug)); err != nil {
			return err
		}
		result, err = s.x.Logo.ReplaceIn(ctx, tx, locked, prepared)
		return err
	})
	// Publication inside the callback remains subject to the outer commit.
	// Preserve both the result and transaction error for reconciliation.
	return result, err
}
```

Inspect the returned operation and transaction outcome together. A callback result
marked `Published` remains subject to the outer commit. This example leaves failed
preparations for reconciliation; it never discards an uncertain publication.

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#reconcile-files -->
```go
func ReconcileFiles(ctx context.Context, manager *attachments.Manager) ([]attachments.ReconcileResult, error) {
	return manager.ReconcilePending(ctx, 100)
}
```

The storage disk must implement conditional creation and reading. Missing either
capability rejects manager construction. Conditional deletion selects pinned
cleanup (including AWS); disks without it, including R2, select write-once cleanup.
The workflow commits a `writing` intent before `Put` with an absence condition.
Successful storage is pinned by ETag and optional version, then journaled as
`stored`. A separate owner-locked transaction publishes `ready` membership and
retires superseded files as `cleanup`. Old files are deleted only after that
transaction's commit is confirmed. Pinned cleanup uses their exact ETag or
immutable version. Write-once cleanup uses an unconditional delete after the
row enters `cleanup` or an irreversible `cleaned` tombstone.

Original keys are `foundry-attachments/<scope>/<file ID>`. New variant keys are
`foundry-attachments/<scope>/variants/<variant ID>`, with a fresh ID for every
generation. Each row commits before its single `IfAbsent` put, and neither key
is replaced in place or reused by another row. Terminal rows cannot become live
again, so unconditional deletion cannot remove a later live generation. Keep
these prefixes exclusively owned by attachments; external writers must not
replace objects there. Reads still use their pinned validators.

After a rejected put acknowledgement or publication, the writer checks the row
again. If settlement fenced the writer, cleanup first commits a tombstone and
then deletes that writer's key once `CleanupGrace` has elapsed. A late create is
never adopted. If a provider completes after the writer exits, `ReconcilePending`
recovers the object through its orphan sweep. Database or storage failures leave recovery to later calls.

The orphan sweep lists at most `limit` objects in one registered disk and scope
per call, under `foundry-attachments/<scope>/` only. It rotates scopes and keeps
per-scope cursors in the manager, so repeated scheduled calls progress past
live objects. Restarting the manager restarts those cursors safely. An object
must be older than both `StoredGrace` and the disk settlement delay
(longest registered disk timeout plus `SettleAfter`). Only canonical original
or variant keys with a durable `cleaned` tombstone in this database are
eligible, and that tombstone must also satisfy `CleanupGrace`. Keys with no
row are never deleted: they may belong to another database or an aliased disk
sharing this namespace. `InspectStorage` reports those unknown keys. All other
rows, including `writing`, `uncertain`, `stored`, `ready`, and `retained`, prevent
orphan deletion. Journal cleanup runs separately and remains retryable.

Explicit collections use the same preparation boundary. Close the reader after
`Prepare` returns, then pass the prepared value into the transaction:

<!-- compiled-source: tests/fixtures/consumer/profiles/attachment_recipes.go#prepare-collection-file -->
```go
func (s Services) PrepareDocument(ctx context.Context, profile Profile, file attachments.FileSource) (attachments.Prepared[Profile, model.ID[Profile]], error) {
	reader, err := file.Open(ctx)
	if err != nil {
		return attachments.Prepared[Profile, model.ID[Profile]]{}, err
	}
	defer reader.Close()
	return Documents.Prepare(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), attachments.Upload{
		Source: reader, OriginalName: file.Name(), ContentType: storage.MediaType(file.ClientContentType()),
	})
}
```

<!-- compiled-source: tests/fixtures/consumer/profiles/attachment_recipes.go#publish-collection-file -->
```go
func (s Services) AddDocumentIn(ctx context.Context, tx *database.Tx, profile Profile, prepared attachments.Prepared[Profile, model.ID[Profile]]) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.AddIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), prepared)
}

func (s Services) ReplaceDocumentsIn(ctx context.Context, tx *database.Tx, profile Profile, prepared attachments.Prepared[Profile, model.ID[Profile]]) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.ReplaceIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), prepared)
}
```

To publish inside a caller's transaction, split the workflow in two.
`Prepare` runs the intent, storage and pin steps in the manager's own
transactions before that transaction begins and returns a `Prepared` upload; it
locks an existing owner while recording the intent, so the per-owner bound stays
exact, and accepts a model the transaction will create, with its key chosen
first. `ReplaceIn` and `AddIn` then publish it inside the caller's transaction
of the extension store's pool, joined through a savepoint, using only that
transaction's connection and at any isolation level, since the upload was
committed before its snapshot. Preparing after a repeatable-read snapshot instead
returns `fault.Invalid` with “prepared upload is not visible to this transaction;
prepare it before the transaction begins”. The result remains `Unpublished` and
retains its operation ID; `errors.As` exposes the underlying `*fault.Error`.
Framework diagnostics survive containment; application hook errors retain their
causes through `errors.Is/As` without exposing their text. They lock the owner, recount its intents and
publish; the attachment is part of the caller's commit. A failed or rolled-back
publication leaves the upload `stored`, ready for another attempt; `Discard`
retires it at once, and `ReconcilePending` retires it after `StoredGrace`. Physical
deletion then waits `CleanupGrace`. Old-file cleanup and unqueued variant generation run after the commit, so a failure there
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

`PendingCleanup` contains durable operations whose object cleanup remains open,
including operations waiting for `CleanupGrace`; waiting is not a cleanup error.
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

`Config.CleanupGrace` (`features.attachments.config.cleanup_grace` in application
configuration) sets the minimum time before physically deleting a retired or
replaced object. Its default is `0`, suitable for development; negative durations
are rejected. Configure **at least 72 hours** when backups copy objects daily,
and keep the grace longer than the object-copy interval plus an operational
margin. For example:

```toml
[features.attachments.config]
cleanup_grace = "72h"
```

The grace applies to originals, replaced variants, reconciliation and stale-writer
tombstones. It starts from the retirement journal timestamp, not the object's
creation time; later journal updates can conservatively extend the wait. Repeated
grace-only reconciliation leaves the timestamp and attempt count unchanged.
Publication and logical removal take effect immediately. The original remains
pending until its retired variants are cleaned. Run `ReconcilePending` periodically
to finish deferred cleanup, including after a queued reconciliation job completes
during the grace window. Keep the same grace on all managers using a database.

Backups must copy objects without deletion (`rclone copy`, not `sync`). The backup
bucket's `objects/` prefix is not pruned in this release, so budget for accumulating
retired objects. The grace supplies a copy window; it does not replace monitoring
successful database and object-backup freshness. See the [storage backup guidance](storage.md#attachment-backups).

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

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go#attachments-thumbnail -->
```go
var Thumbnail = attachments.DefineVariant("thumbnail",
	imaging.NewPlan().Fit(320, 320, false).Format(imaging.WebP))
var Photos = attachments.Define(ext.Owner, "photos", attachments.Policy{
	Disk: storage.DefineDisk("photos"), Cardinality: attachments.Multiple,
	Accepted: []storage.MediaType{"image/jpeg", "image/png"},
	Variants: []attachments.Variant{Thumbnail},
})
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
`000002_create_variants`) and a generation-specific object key under
`foundry-attachments/<scope>/variants/`. Existing keys under
`foundry-attachment-variants/<scope>/` remain readable and use normal journal
cleanup; the orphan sweep never lists that legacy prefix. Regeneration moves
variants to the new prefix without changing the database schema.
It is written with an absence condition and becomes ready only while its
original is still ready, replacing the previous ready variant of that name.
Variants follow the original: replace, detach, clear, owner deletion and orphan
pruning delete them before the original, and the original stays `cleanup` (and
is retried) until every variant is confirmed deleted. `DetachKeepFile` hands
over only the original; its variants are removed. `ReconcilePending` retries
variant cleanup and settles aged `writing`/`uncertain` variant intents with the
same cutoff as originals; because an unresolved variant is never ready, a
present object is deleted rather than published.

Loaded attachments (`Collection.Load`, `List`, `First`, `Find`, `LoadLocalized`) carry their
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

<!-- compiled-source: tests/fixtures/goway/guideadapters/slots_test.go#attachments-queued-photos -->
```go
func queuedPhotos(producer *jobs.Outbox) (attachments.Collection[ext.Article, int64], error) {
	job := attachments.DefineReconcileJob("attachments.reconcile", jobs.DefaultPolicy("maintenance"))
	queue, err := job.ToOutbox(producer)
	if err != nil {
		return attachments.Collection[ext.Article, int64]{}, err
	}
	return Photos.WithReconciliation(queue), nil
}
```

Register `job.Declare(manager)` in the ordinary job registry before dispatching its queue.
Binding a queue does not change collection identity or policy. Cleanup and job
outbox rows commit in the same transaction, including owner lifecycle cleanup.
Normal synchronous cleanup still runs after commit; the queued job is an
idempotent recovery path. A rollback publishes neither row. Configure and run the
existing outbox publisher and worker explicitly. A periodic ordinary job can call
`ReconcilePending` to recover intents and orphan objects left by a crashed
publisher. Aged unresolved writes with ambiguous storage still require operator
settlement.

<a id="recipe-read-files"></a>
## Reads and localization

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#transaction-current-files -->
```go
func CurrentFiles(ctx context.Context, tx *database.Tx, manager *attachments.Manager, bound ArticleSlotHandles, article Article) (value.Optional[attachments.Attachment[Article, model.ID[Article]]], []attachments.Attachment[Article, model.ID[Article]], error) {
	owner := ArticleIdentity.ReferenceOf(article)
	logo, err := attachments.OneCollection(bound.Logo).FirstIn(ctx, tx, manager, owner)
	if err != nil {
		return logo, nil, err
	}
	gallery, err := attachments.ManyCollection(bound.Galleries).ListIn(ctx, tx, manager, owner)
	return logo, gallery, err
}
```

`List`, `First`, `Find` and `Collection.Load` recheck current owner visibility. `Collection.Load` takes
up to 1000 owner references, uses one repeatable-read snapshot and two SELECTs
for a nonempty active set, and returns an in-memory batch. Batch access performs
no I/O and returns copied slices. The aggregate limit is 4096 files and 4 MiB of
properties. A missing or soft-deleted owner differs from an active empty
collection. Snapshot results do not replace fresh authorization checks.

`FirstIn(ctx, tx, manager, owner)` and `ListIn(ctx, tx, manager, owner)` read
through a transaction of the extension store's exact pool, including that
transaction's uncommitted attachment changes. They keep the existing owner
visibility, locale, ordering, variant and row/property bounds, and preserve the
caller's isolation level. They do not acquire an owner lock. For a check followed
by a business update, lock the owner in that transaction before checking its
attachments, and keep the check and update in the same transaction. Every
competing mutation must use the same owner lock before its own business checks.
For slots, use `attachments.OneCollection(handle).FirstIn` or
`attachments.ManyCollection(handle).ListIn`; loaded slot values remain snapshots.

`ReadBytes` rechecks ready membership, conditionally reads pinned storage, and
verifies the complete checksum and length (a disk-verified full read whose
stored checksum equals the pin is not hashed again). `Image` applies an explicit
plan to those bytes. `Open` streams an attachment of any size up to the policy
bound, pinned to its ETag/version and verified at EOF; only the lookup uses
manager read admission, and the reader holds one disk stream slot until closed.

`PublicURL` and `TemporaryURL` recheck ready membership and derive the link from
the stored key and version; keys are immutable per upload, so no storage request
is made. For attachments already loaded through `Collection.Load`, `List`, `First` or
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
locales remain stored but unavailable until supported again. [Message catalogs and request locale resolution](localization.md) use the same locale identities; attachment collections keep their explicitly selected locale.

`Matching` provides a typed materialized owner query predicate for a collection
and exact locale. It is bounded to 4096 files/1000 distinct owners. The later
ordinary model query keeps its own visibility and authorization filters; the
predicate is a captured membership list, not a live subquery.

<a id="recipe-retire-files"></a>
## Deletion and inspection

<!-- compiled-source: tests/fixtures/consumer/profiles/attachment_recipes.go#retire-collection-files -->
```go
func (s Services) DetachDocumentIn(ctx context.Context, tx *database.Tx, profile Profile, id attachments.ID[Profile]) (attachments.ChangeResult, error) {
	return Documents.DetachIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), id)
}

func (s Services) ClearDocumentsIn(ctx context.Context, tx *database.Tx, profile Profile) (attachments.ChangeResult, error) {
	return Documents.ClearIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile))
}
```

<!-- compiled-source: tests/fixtures/consumer/articles/attachment_recipes.go#clear-slot-files -->
```go
func (s *Service) ClearFiles(ctx context.Context, tx *database.Tx, visible orm.Query[Article], id model.ID[Article]) error {
	article, err := orm.RequireLocked(ctx, tx, orm.Lock(visible.Where(orm.KeyEq(Articles.Key(), id))))
	if err != nil {
		return err
	}
	if _, err := s.x.Logo.ClearIn(ctx, tx, article); err != nil {
		return err
	}
	_, err = s.x.Galleries.ClearIn(ctx, tx, article)
	return err
}
```

`Detach` and `Clear` retire ready ownership transactionally, then clean storage.
`DetachIn(ctx, tx, manager, owner, id)` and
`ClearIn(ctx, tx, manager, owner)` join a caller transaction through a savepoint.
Both lock the owner until the caller transaction ends, retire membership and
queue any reconciliation work inside it, and clean storage only after commit.
`OneHandle` and `ManyHandle` expose `ClearIn(ctx, tx, owner)`. A rollback restores membership and suppresses cleanup.
A successful call reports `Published` subject to the caller's commit;
`PendingCleanup` lists the retired operations awaiting that commit. Cleanup
failures surface as the transaction's after-commit error and leave durable
intents for `ReconcilePending`. The existing cleanup grace still applies. See the
[single-connection consumer example](../../tests/fixtures/consumer/articles/writes_postgres_test.go).

`DetachKeepFile` instead records durable `retained` state and returns explicit
storage pins. Cleanup responsibility passes to the application. Owner deletion,
orphan pruning and reconciliation preserve that object. `RetainedObject` recovers
its pins after an uncertain detach commit. Retained state is distinct from
cleaned; no file deletion is implied.

Call `attachments.Cleanup` from the owner's ORM `Deleted` observer with the
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
stored collection names no registered collection declares; it has no delete flag. Register it explicitly in the [typed command kernel](developer-tooling-and-testing.md#commands-and-bootstrap).

`InspectStorage` lists one bounded provider page under the selected registered
owner's framework prefix and a registered collection disk. Its opaque cursor
retains the first-page age cutoff and disk/owner policy. Every journal state
protects its key. `Unknown` counts age-qualified untracked objects in that page;
`Candidates` supplies their keys and metadata as bounded inspection samples. Sum
`Unknown` while following `Next` for a complete scan count. Counts reflect this
disk ID and database only, so aliases and other databases may own these objects.
They are never deleted automatically. This listing is not a transactional snapshot
and does not authorize deletion.

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

## Test missing backing objects

Use `testkit/attachments.RemoveBackingObject(t, manager, file)` to simulate a
ready attachment whose original object has disappeared. Pass `stored.File` for
an `attachments.Attachment` returned by a collection, or a file loaded into a
model slot. The helper resolves the recorded storage identity through the
manager's typed internal store. It preserves attachment metadata and variants,
and deletes only the pinned original version or ETag. Unsupported conditional
deletion and changed validators fail explicitly. A later `ReadBytes` or open of
that file then returns an error matching `storage.NotFound`.

Here `attachmentstest` imports
`github.com/weiloon1234/Foundry-Go/testkit/attachments`. Use isolated test-owned
files and storage. The helper mutates real storage and leaves the attachment
record ready so the application can exercise its missing-object response.
Storage pins remain private, and helper failure output omits underlying causes.
The manager's test seam requires a module-internal token, following the event
test helper pattern. It does not add a production storage-pin accessor.

For application reads inside deferred HTTP sources, use the
[storage HTTP error mapper](storage.md#http-and-links) after handling domain errors.


The ORM examples are checked against the [compiled guide examples](../../tests/fixtures/goway/guideadapters) and the linked consumer sources.
