# 18 — Imaging and model extensions

## Imaging expansion — 2026-10-03

The requested continuation expands the existing public `imaging` package toward
Intervention Image coverage. The original milestone below is historical; it does
not establish acceptance of this continuation. Consumers keep Foundry types,
immutable plans, configured application ownership and attachment integration.

The user approved a portable default engine plus an optional libvips backend.
Native installation is opt-in. Capabilities must describe actual backend support,
including separate read/write, animation and color-management support; selecting
an unsupported capability must fail explicitly. Existing portable deployments
must continue to build without native libraries.

Implementation and acceptance proceed in coherent stages:

1. Everyday editing: proportional/exact downsize and enlargement, positioned
   fill/crop, padding and canvas changes, resampling selection, arbitrary rotation,
   immutable image insertion, opacity, blend modes, masks and additional effects.
   Include blank image creation, pixel assertions, allocation preflight, concurrent
   plan reuse, independent consumers and real editor completion.
2. Text and drawing: bounded font ownership, text layout/wrapping/alignment,
   shapes and paths, usable with the same immutable plans and attachments.
3. Portable formats and animation: AVIF input, lossy WebP output, preserved
   animations where codecs support them, format capabilities and encoding controls.
   Verify actual decode/encode/transform round trips and container limits.
4. Optional native backend: additional formats (including HEIC/HEIF, JPEG 2000,
   JPEG XL and SVG rasterization where installed libvips supports them), color
   management, metadata policy, smart cropping and backend capabilities. Preserve
   bounded work and lifecycle ownership and test portable and native builds.
5. Consumer/attachment integration and final audit: reusable variants, failure
   paths, API documentation, compiler/editor contracts, relevant fuzz/race/backend
   checks and `make verify` against the final source. Record actual evidence here.

Ordinary upscaling is resampling. AI super-resolution requires a separately chosen
model/runtime and is not implied by resize or sharpen; evaluate that extension
separately instead of claiming reconstructed detail from interpolation.

Status: stage 1 editing is accepted. Pixel/limit regressions, the complete
imaging race suite, bounded geometry fuzzing (37,066 executions), real imaging
editor probes, independent consumer/compiler checks and PostgreSQL attachment
variant checks passed. Both the initial and final `make verify` gates passed
(exit 0), including the source audit corrections. Stage 2 text/drawing also passed
`make verify`, imaging races, 72,129 text/path fuzz cases, five real editor probes,
consumer/compiler checks, PostgreSQL attachment checks and visual output review.
Stage 3 now includes accepted GIF/APNG preservation and format capability
snapshots: `make verify`, races, 332,702 animation fuzz cases, six editor probes,
consumer/compiler checks, persisted animated attachment variants and independent
Pillow decoding all passed. Portable AVIF still/sequence input is also accepted:
`make verify`, imaging races, 724,045 container fuzz cases, independent consumers,
persisted PostgreSQL variants, CGO-disabled builds and 26 independently decoded
fixtures passed. Exact high-depth preservation and decoded timeline checks are
covered. PNG/APNG compression and AVIF speed/independent alpha quality controls
also passed final `make verify`, races, consumer/compiler/editor and PostgreSQL
checks. Encoder auditing fixed ignored writer failures and mismatched WebP
container/coded dimensions; 543,771 WebP header fuzz cases passed. Portable
WebP animation is also accepted: final `make verify` (including the complete
real-gopls suite), imaging races, 943,806 header/pipeline fuzz cases, CGO-disabled
builds, independent Pillow/libwebp decoding and PostgreSQL variants passed.
The audit corrected VP8 video-range conversion/chroma interpolation and added
padded macroblock/fixed encoder scratch admission. Portable lossy WebP is now accepted too: the approved codec replacement passed
full `make verify` with PostgreSQL and real-gopls, imaging races, 208,840 encoder
fuzz cases, CGO-disabled builds and independent libwebp decoding. Typed mode,
quality and method controls apply to stills and every animation frame; lossless
remains the default. The audit corrected codec RIFF padding and JPEG color-range
handling. Stages 4–5 are now accepted. Libvips 8.18.7 and pkgconf 3.0.7 are installed;
the optional backend passed native/portable builds, HEIF/JP2/JXL/SVG round trips,
ICC/metadata and smart-crop checks, configured consumers, upload dimensions,
PostgreSQL attachments, races and editor/compiler contracts. The final parser
audit passed 294,038 fuzz executions. Native writers probe actual encoding and ICC
round trips; unsupported metadata preservation rejects explicitly. The final full
`make verify` passed with PostgreSQL and real gopls, and all 157 final source and
fixture fingerprints match. See
[current evidence](../docs/evidence/imaging-expansion-20261003.json); the original
milestone's historical completion remains separate from this expansion's evidence.

The configured consumer now exercises enlargement/padding through
`application.New` and `Services.Image()` over authenticated HTTP, verifies
persisted pixels and confirms that application shutdown closes its engine.
The focused PostgreSQL race test, real editor probe, consumer generation freshness
and documentation checks passed. The completed native consumer also exercises
configured ownership, smart cropping, ICC conversion and borrowed upload dimension
validation. Attachment variants share the engine's detected media and capabilities.

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

The original image milestone supported the default input/output formats,
including output-only AVIF and lossless WebP; the expansion above supersedes
that AVIF input boundary. Codec round trips across all eight output formats and image limits passed
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
