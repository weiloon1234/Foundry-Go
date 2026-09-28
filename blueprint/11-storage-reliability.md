# 11 — Storage reliability

## Purpose and prerequisites

Prerequisites: [02](02-foundation-and-application-lifecycle.md), [08](08-http-validation-and-responses.md). Make storage reliability measurable for local files, AWS S3 and Cloudflare R2.

Rust references: `src/storage`, `src/imaging`, `tests/attachments_acceptance.rs`, `http_edge_acceptance.rs`; `docs/guides/storage-and-imaging.md`. Inspect the Rust adapter's buffering defaults and S3 multipart implementation as design inputs, not evidence that Rust itself is unstable.

## Public contracts and package boundaries

`storage` owns disk registration, typed `DiskID`/`ObjectKey`, metadata, transfer options, result and error contracts. `storage/local` and `storage/s3` own provider implementation. R2 uses an explicit S3-compatible provider profile with its own verified capabilities, not an assertion of complete S3 equivalence.

Core streaming shape:

```go
Put(ctx context.Context, key ObjectKey, source io.Reader, options PutOptions) (StoredObject, error)
Open(ctx context.Context, key ObjectKey, options ReadOptions) (io.ReadCloser, ReadInfo, error)
```

These are interface signature fragments. Bytes/file conveniences delegate to the streaming core. Returned readers are caller-owned and must be closed. Framework HTTP download helpers close them automatically. Typed options cover visibility intent, content type, size, checksum and conditional writes; arbitrary provider headers remain an explicit adapter escape hatch.

## Implementation slices

1. Typed keys, error taxonomy, capability descriptions, disk config and a fault-injection adapter contract suite.
2. Local streaming with root confinement, symlink escape prevention, temporary-file cleanup and atomic replacement within the same filesystem.
3. AWS S3 adapter using the official AWS SDK for Go v2 after checking existing dependencies for overlap, credential-provider chain, custom endpoints, multipart upload/abort, paginated listing and signed URLs.
4. R2 profile and real-provider verification for supported operations, signing, URL encoding, endpoints and provider-specific options.
5. HTTP multipart integration, upload policies, bounded buffers/spooling, download streams and lifecycle cleanup.

## Reliability semantics

- Never implement a method named streaming by reading the whole object into memory. Bound memory by configured buffer/part size and concurrency, not object size.
- Retry only operations whose source can be replayed safely. Non-seekable streams require bounded spooling or an explicit non-retryable result; never resend an already-consumed stream as if intact.
- Cancellation closes internal resources and aborts multipart transfers where possible. Report cleanup failures alongside the primary error; expose enough identifiers for later reconciliation.
- Distinguish not found, forbidden, unsupported capability, conflict, retryable transport failure and ambiguous completion. Do not infer nonexistence from permission errors.
- Cross-object moves are copy-then-delete unless the provider offers atomic semantics. Do not promise atomic rename across object stores.
- Public visibility is access intent, not a requirement to send object ACLs. R2 and modern S3 configurations may rely on bucket policy/CDN access.
- Separate object keys from URLs. Encode URL path segments correctly and reject filesystem traversal; do not normalize hostile keys into a different authorized object.

DB and object storage cannot share a transaction. The attachment milestone adds staged state, compensation and reconciliation. Storage itself must return stable object/version information for those operations.

## Provider acceptance matrix

Run one shared suite for local/S3/R2 capabilities, plus provider-specific tests. Cover small/large/empty objects, unknown-length sources, canceled reads/writes, part failure, abort failure, retry boundaries, credential failure, overwrite preconditions, list cursors, ranges where supported, Unicode/spaces in keys, signing expiry and memory bounds.

Use real AWS/R2 test namespaces for provider certification; an emulator does not certify either provider. Missing credentials mean certification is pending, not passed. Never log credentials or delete objects outside the test-owned prefix.

References: [AWS SDK S3 utilities](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/sdk-utilities-s3.html), [R2 S3 compatibility](https://developers.cloudflare.com/r2/api/s3/api/). Apply the [common gate](README.md#common-completion-gate).


## Current implementation and verification

Milestone 11 is complete: implementation, native acceptance and separate live
AWS S3/R2 checks passed; the independent consumer experience was reviewed.
The [storage guide](../docs/guides/storage.md) owns public contracts,
provider differences, operational limits and test configuration.

Delivered code and reviewed consumer experience:

- Shared typed keys, disks, options, metadata, errors and mutation outcomes;
  bounded operations/readers, registries, lifecycle, file/byte/copy/move helpers,
  full-read checksums, pinned seekable streams and explicit URL disclosure.
- Managed local store with confined roots, one-file metadata/payload publication,
  atomic replacement, cross-process locks, bounded listing and staging cleanup.
  Local support targets macOS/Linux; managed stores are not plain asset folders.
- Official SDK S3/R2 profiles, scoped keys, ranges, bounded listing, serial multipart
  buffers, upload admission, replayable-part retries, abort confirmation, scoped
  recovery and presigned reads. Root and consumer dependency graphs are aligned.
- Existing HTTP upload/download composition, shared adapter testkit and independent
  consumer lifecycle/upload examples. No starter project was introduced.
- R2 conditional single-request writes have a declared size limit. Helpers infer
  known sizes before capability checks; unsupported multipart/delete/version
  combinations fail explicitly. Physical object identity protects moves through
  different names/namespaces that alias one object.

Native verification on macOS (Go 1.27.1, default compiler/GC/CPU settings):

- The first complete `make verify` passed in 760.1 seconds with existing local
  PostgreSQL/Redis required, format/vet, root and consumer behavior, current
  generation and documentation checks. All 810 invalid API cases, 310 actual
  gopls probes and six automatic getter/setter field notices passed.
- Storage and consumer race suites passed. After source review, regression tests
  additionally verified inferred sizes for conditional file/copy helpers,
  malformed provider metadata classification and preservation of an already
  applied write when a later metadata request reports a missing bucket.
- A subsequent full regression exposed a reader-shutdown ordering race. Reader
  close now cancels the backend context synchronously before interrupting the
  body. The deterministic reader/disk regression passed 100 race-enabled
  repetitions, followed by complete storage/consumer races and live R2 checks.
- Both storage editor probes passed again after those fixes. Targeted key/range
  fuzzing passed 217,434 and 422,734 inputs respectively.
- On this Apple M4 Max, local 1 MiB/16 MiB streaming uploads allocated about
  38 KB/op in both cases (91 allocations); measured throughput was approximately
  69/511 MB/s with file/directory sync enabled. Measurements ran alongside other
  checks, are not deployment guarantees and do not measure peak retained memory.

Private run evidence is under `.cache/milestone11-storage/`. The final repository
check records an unchanged source fingerprint after the reviewed fixes and
documentation update.

### Live R2 findings

Live Cloudflare R2 verification passed with a user-supplied temporary credential
on 2026-09-16. The final race-enabled provider run took 31.1 seconds and verified ordinary,
empty and Unicode objects; conditional writes; ranges; pagination; multipart
publication and abort cleanup; signed reads; and public development URL reads.
Payload length and SHA-256 matched through both URL paths. Every test-created
object/upload was confined to a random namespace and cleaned up. Conditional
delete remained an explicit unsupported-capability skip.

Live tests exposed two provider differences now covered by regressions:

- R2 returns an upload-generation header on writes without retaining addressable
  historical versions. Follow-up reads use ETags; `ObjectInfo.Version` stays empty
  for R2. AWS retains its version selection behavior.
- R2 [normalizes Unicode names](https://developers.cloudflare.com/r2/reference/unicode-interoperability/).
  The profile declares `RequiresNFCKeys` and rejects other spellings in keys,
  namespaces, prefixes, reads, deletes, signing and maintenance before I/O.
  Tests prove rejected aliases preserve the canonical object. Local/AWS key
  identity remains byte-exact; no physical-key encoding or silent normalization
  is introduced. Existing `golang.org/x/text` supplies NFC validation.

### Live AWS findings and acceptance

The race-enabled AWS S3 suite passed in 10.1 seconds against a dedicated private,
versioned general-purpose bucket. It verified ordinary/empty/Unicode objects,
conditional writes/reads/deletes, ranges, paginated listing, multipart publication,
interrupted-source aborts, signed download length/SHA-256, historical version reads,
and encoded-key object/multipart pagination. The configured-public-read case
skipped explicitly because this bucket blocks public access; no policy was weakened
for the test. Public URL identity was exercised against R2 and SDK wire peers.

The first AWS run found that listing responses encode spaces as `+` and literal
plus signs as `%2B`. Path-style decoding changed object identity in listing and
left four test versions behind during cleanup. AWS object/upload/version keys and
key markers now share one form-style decoder; opaque continuation/upload/version
IDs remain unchanged. R2's existing path-style decoder remains intact. Native
regressions cover both profiles, percent sequences, Unicode, multipart cursors and
version cleanup. The four exact journaled versions were removed and the successful
rerun cleaned all its own data. A final read-only inventory verified no objects,
versions, delete markers or uploads remained in the dedicated test prefix.

No new dependencies, bucket provisioning, public access changes or application
boilerplate were needed. The scoped IAM policy correction was explicitly approved
and saved before testing. Private credential copies are removed after verification;
credentials are never placed in framework sources or evidence logs. Native final
acceptance is recorded against an unchanged source fingerprint in private evidence.
Milestones 12–24 and the final framework-wide audit remain open; 25 stays deferred.


## Rust storage parity review

| Rust source/behavior | Go destination and deliberate change |
|---|---|
| `StorageAdapter` bytes/file/stream operations | `storage.Backend` streaming is mandatory; conveniences delegate, never implement streaming with whole-object buffering. |
| `StorageDisk` callback boundaries and metadata | `Disk` adds typed keys/options, callback isolation, bounded ownership, checksums, explicit mutation outcomes and returned-reader cleanup. |
| Manager/disk lookup and visibility | Explicit descriptors, immutable duplicate-checking `Registry`, typed foundation modules and private-by-default public-link policy. |
| Local filesystem writes/reads/copy/move | Managed one-file metadata/payload format with confined root, atomic publication and process locks; `CopyTo`/`MoveTo` preserve exact object identity and partial outcomes. |
| Local configured asset URL | Existing typed HTTP route URLs and signed endpoints compose with `storage/http.Download`; managed storage is never exposed as a plain directory. Rust local temporary URLs were unsupported. |
| S3 multipart and URLs | One official SDK adapter with bounded reusable buffers, safe part retries, explicit abort/reconciliation, public CDN base and read-only SDK presigning. |
| Prefix listing and continuation | Typed bounded pages/cursors with exact prefix/provider scope; no unbounded recursive materialization. |
| Upload/download consumer use | Existing typed multipart capture/validation and file responses remain the transport implementation; `StoreUpload`, `Download`, `Stream` compose them. |
| R2 through S3 compatibility | Explicit R2 region/endpoint/credentials, supported single-request preconditions, and declared multipart/delete/version limits. No blanket compatibility claim. |

Backend operations preserve context and ordinary Go errors. Process locking,
callback failures, SDK transport effects and consumer/editor use passed native
checks. Separate live AWS S3 and R2 verification also passed.
