# 18 — Imaging and model extensions

## Purpose and prerequisites

Prerequisites: [06](06-relations-and-advanced-queries.md), [07](07-model-lifecycle-events-and-audit.md), [11](11-storage-reliability.md), [12](12-jobs-and-worker-kernel.md). Provide reusable model capabilities so applications do not rebuild infrastructure tables and file workflows.

Rust references: `src/imaging`, `attachments`, `metadata`, `translations`, `settings`, `countries`; `tests/attachments_acceptance.rs`, `support_stores_acceptance.rs`; `docs/guides/model-extensions.md`, `blueprints/17-framework-models-image-module.md`.

## Contracts and ownership

Keep imaging, attachments, metadata, model translations, settings and country reference data in focused public packages. Share registered typed owner/key descriptors and model lifecycle integration. Framework migrations/models are framework-owned and versioned.

Attachment collections bind their owner model, collection identity, cardinality, accepted media and transformation policy. Typed metadata/settings keys bind value codecs; unrestricted JSON is an explicit dynamic facility rather than the default API.

Typed collection usage, exercised by the independent consumer:

```go
result, err := UserAvatar.Replace(ctx, manager, user.FoundryReference(), upload)
```

`UserAvatar` owns the validation/transformation policy. Upload endpoints do not duplicate it. The collection descriptor must reject a different model owner at compile time.

## Implementation slices

1. Image inspection/decoding with byte, dimension, pixel, frame and memory limits; orientation, resize/crop, format conversion and metadata policy. Reproduce supported Rust transforms/formats after dependency reuse review and codec verification.
2. Typed attachment collections, file metadata, upload hooks and replacement operations using the storage API.
3. Staged upload/attachment state, transactional ownership changes, post-commit cleanup and retryable reconciliation jobs.
4. Typed metadata keys, translated model fields, locale validation hooks, eager/batch loading, settings descriptors and country model/seeder.
5. Extension query scopes, soft-delete behavior, owner deletion cleanup and orphan inspection commands.

## Consistency and failure behavior

Persist staged upload intent before claiming an attachment is ready. Finalize ownership only after successful storage, and delete superseded objects after the ownership transaction commits. On failure, preserve the previous attachment and schedule cleanup/reconciliation for orphaned new objects. Object deletion failure must not be hidden or cause deletion of the wrong current object.

Image transforms have bounded concurrency. Reject oversized/decompression-bomb inputs before expensive processing where possible. Treat user-supplied MIME type and extension as hints, not trusted media identity.

Translation locale IDs are typed and validated through an injected locale catalog contract; milestone 20 supplies complete request/catalog integration. Keep this distinct from frontend message translations. Country data is versioned reference data with explicit seeding, never silently installed on every boot.

## Acceptance

Test collection ownership/type mismatch, cardinality, simultaneous replacements, failed storage/commit/delete, orphan reconciliation, cancellation, media limits, malformed images, transform output, typed metadata decoding, translation fallback, eager-load query counts, soft-deleted owners and idempotent reference seeding. Apply the [common gate](README.md#common-completion-gate).

## Delivered behavior and verification

Delivered packages include `imaging`, `attachments`, `extensions`, `metadata`,
`translations`, `settings`, `countries` and the shared `i18n` locale contract.
Framework tables use ordinary generated model declarations and explicit
versioned migrations. [Image details](../docs/guides/imaging.md),
[attachment recovery](../docs/guides/attachments.md) and
[model extension ownership](../docs/guides/model-extensions.md) describe the
current boundary. The independent `profiles` consumer demonstrates policy and
transactional lifecycle composition.

Image source supports the Rust default input/output formats, including
output-only AVIF and lossless WebP. Codec round trips across all eight output formats and image limits passed
focused verification. Attachment cleanup preserves uncertain writes for explicit
settlement, pins acknowledged objects and separates committed ownership from
cleanup failure. Read-only provider orphan inspection never assumes that age
alone authorizes deletion. `DetachKeepFile` records deliberate retention and
transfers cleanup responsibility explicitly. Country seeding preserves
application-managed activation, conversion rate and default choices.

Written acceptance cases cover typed policy/identity, detected media and image
output, batch/localized query counts, concurrent replacement, failed storage,
hook/commit rejection, both outcomes of a lost commit acknowledgement, retained
files, cancellation with actual callback lifetime, owner lifecycle rollback,
orphan paging, and real PostgreSQL/outbox/Redis worker retry. Compiler rejection,
actual editor probes and generation targets are registered. Dependencies and
framework/consumer generation are complete. Focused root and independent
consumer tests, all 15 new compiler failures and seven actual editor probes
passed. The test/fix phase corrected translation cleanup beyond 1,000 rows,
UUID orphan pagination, and empty-reader progress handling. Source and consumer
review, focused races, bounded image fuzzing and the final full native milestone
gate passed. See [the recorded evidence](00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review).
