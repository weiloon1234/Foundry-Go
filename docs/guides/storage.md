# Typed storage

**Milestone 11 passed live Cloudflare R2 and AWS S3 verification.** Native
framework/consumer checks and the scoped cloud suites cover the contracts below.
The [owning blueprint](../../blueprint/11-storage-reliability.md) records the
evidence, including AWS versioned reads and cleanup, and provider limitations.

`storage.Disk` owns operation limits, cancellation and returned readers.
`storage/local` supplies a managed filesystem store. `storage/s3` uses the official
AWS SDK with separate AWS and R2 profiles. Applications declare their disks once,
select their adapters and write domain policies; they do not implement transfers,
multipart cleanup or HTTP file transport.

## Ordinary application operations

These fragments show the public API. The verified
[independent consumer](../../tests/fixtures/consumer/storing/files.go) supplies
complete imports and application assembly.

```go
var Documents = storage.DefineDisk("documents")

key, err := storage.ParseKey("reports/annual.pdf")
if err != nil { return err }

stored, err := disk.Put(ctx, key, input, storage.PutOptions{
    ContentType: "application/pdf",
    Condition:   storage.IfAbsent(),
})
if err != nil { return err }

body, info, err := disk.Open(ctx, key, storage.ReadOptions{})
if err != nil { return err }
defer body.Close()
// Stream body to the application-owned destination.
_ = stored
_ = info
```

An `ObjectKey` preserves exact UTF-8, case and literal percent characters. It is
not a filesystem path or URL. Empty, absolute, traversal, backslash and control
character names are rejected; input is never cleaned into a different object.
`Prefix` is an exact lexical prefix, with an optional final slash. A zero prefix
selects all objects in the configured disk namespace.

R2 normalizes Unicode names to NFC at the service boundary. Its profile exposes
`Capabilities.RequiresNFCKeys` and rejects non-NFC object keys, namespaces and
listing prefixes before provider I/O. It never silently normalizes one spelling
into another object's identity. Reads, deletes, signing and upload maintenance
follow the same rule. Local storage and AWS preserve distinct Unicode byte
spellings. This is a documented [R2 provider difference](https://developers.cloudflare.com/r2/reference/unicode-interoperability/),
not a normalization performed by `ObjectKey`. Keep display filenames separate
from application-selected object keys.

Use `PutBytes` for bytes already in memory. `PutFile` opens an application-selected
regular file, delegates to `Put`, and closes it; it never removes or modifies that
file. Never pass a client filename as its filesystem path. `ReadBytes` requires an
explicit maximum and returns no partial data. Use `Open` for large bodies and
always close its reader. Full-object SHA-256 verification, when metadata provides
it, completes at EOF. A partial read or range is not a full checksum verification.

`Size` and `Checksum` are typed optional declarations. Missing size means unknown
input length; `value.Set[int64](0)` means exactly empty. Streaming size/checksum
failures occur before publication. An ETag is an opaque quoted validator, never
an assumed MD5. Providers may reuse a validator for identical content.

## Conditions, listing and copies

`storage.IfAbsent()` creates only when absent. `storage.IfMatch(etag)` returns a
validated replacement condition. Reads and deletes take their own typed option
structs. Inspect `disk.Capabilities()`; unsupported options fail before consuming
a source or changing the target.

```go
prefix, err := storage.ParsePrefix("reports/")
if err != nil { return err }
page, err := disk.List(ctx, storage.ListOptions{Prefix: prefix, Limit: 100})
if err != nil { return err }
// Use page.Next unchanged with the same disk and prefix for the next page.
```

Listings are bounded pages, not transactionally consistent snapshots. A cursor
belongs to its adapter/bucket namespace and prefix and grants no authorization.
S3 listing obtains complete metadata through bounded, serial conditional HEAD
requests. Concurrently removed/replaced entries are skipped while the provider
cursor advances. Authorization scopes belong in application-selected prefixes
and object policies; a client-supplied prefix must not choose another tenant.

`source.CopyTo(ctx, sourceKey, destination, targetKey, options)` streams a complete
source to another key/disk. It requires capacity on both disks, including two
slots for a same-disk copy. `MoveTo` then conditionally deletes the original. It
requires conditional-read/delete capabilities, preserves replacements whose
validators change, and returns `MoveResult` with separate destination/source
outcomes. This is not an atomic rename. Both adapters must expose `ObjectLocator`,
which supplies their stable store and complete object identities. Aliases of one
object are rejected even across different namespace/disk names; equal keys in
distinct stores work normally. A failed deletion
can leave two complete objects; no automatic rollback deletes the destination.

## Errors and ownership

Use `errors.Is` for ordinary categories, including `NotFound`, `Forbidden`,
`PreconditionFailed`, `LimitExceeded`, `IntegrityFailed` and `Unsupported`.
`Exists` suppresses only a genuine not-found classification. Permission failures
remain errors. Disk outcome inspection is bounded to 256 nodes and 64 unwrap
levels. If no storage classification is reached, a cyclic/deep/wide adapter error
keeps mutation outcomes unknown; failed reads still release their bodies normally.
Reached outcomes and cleanup references survive cancellation. These bounds cover
framework traversal, not work inside custom error methods, which must return.
Local/S3 adapter classification and HTTP download cancellation checks also bound
error traversal. An unclassifiable source error still runs staging cleanup and
retains the adapter's established publication state. S3 classification also isolates
panic and Goexit so they cannot skip multipart abort cleanup.
Use `errors.As` to inspect `*storage.Error` and its mutation outcome:

- `Unchanged`: the requested publication/deletion did not take effect.
- `Applied`: it took effect, but a later metadata, durability or cleanup step failed.
- `Unknown`: the provider outcome cannot be established; reconcile before retrying.
- `NotApplicable`: this operation does not mutate an object.

An error is not an automatic retry instruction. Normal formatting/logging redacts
provider messages, signed URLs and cleanup references. `Unwrap` is for controlled
diagnostics. `Cleanup().Get()` and the reference's explicit `Value()` identify
orphan work; do not place them in ordinary public responses or logs.

`Put` borrows and never closes its input. An uncooperative blocked source can delay
cancellation; its operation slot stays occupied until it actually returns. `Open`
returns an owned reader that holds a slot until close, cancellation or shutdown.
`Disk.Close(ctx)` cancels work and interrupts owned readers. Closing an individual
reader also cancels its backend context before closing the body, so an active read
retains its cancellation cause. A timed-out disk close does not release capacity
early: wait for `Disk.Done()` before closing its backend.

For application assembly use adapter modules (`local.Module`/`s3.Module`) and
`storage.Module` with an explicit dependency on the adapter's provider ID. Factories
perform no I/O; adapters start at boot and close after disks drain. Module cleanup
waits for actual callback exit rather than closing infrastructure beneath it.

## Local managed store

Provision an existing, empty, operator-controlled directory, then use
`local.DefaultConfig(absoluteRoot)`. The adapter rejects unrelated populated roots.
It writes a format marker and private files with 0600/0700 permissions. It never
imports, wipes, or overwrites arbitrary existing directories.

Keys map to SHA-256 filenames. Each published file contains one bounded binary
header, metadata and payload, atomically renamed from a staging file in the same
filesystem. This preserves case/Unicode key identity even on case-insensitive
macOS volumes and avoids a separate metadata/payload commit. `os.Root` confines
filesystem access; advisory locks coordinate publications across processes.
`Sync` defaults on and syncs files/directories for crash durability. This is an
operator-controlled object store, **not a directory of plain uploaded files**.
Back it up as a complete store. Use the HTTP storage bridge to serve content.

Local support currently targets macOS and Linux. Other platforms return
`Unsupported`; no Windows/network-filesystem durability certification is claimed.
Historical versions are not retained. A file opened before replacement can finish
reading that inode; subsequent pinned opens fail when the validator changes.

`Prune(ctx, olderThan, limit)` removes only recognized abandoned staging files,
skips locked active uploads and never removes published objects. Call it through
application maintenance or an explicitly registered [scheduler](scheduler.md) task.
Listing scans bounded directory batches with a bounded selection heap. It is
O(number of managed files) per page. `MaxScan` limits work and fails explicitly
rather than returning an incomplete successful page; size it for the deployment.

## AWS S3 and Cloudflare R2

Both profiles use one official AWS SDK dependency family, with no alternate S3
client library. Configure an existing general-purpose bucket and optional trailing
slash `Namespace`; the namespace is prepended once by the adapter. There are no
bucket creation/deletion or policy/ACL mutation methods. Directory buckets,
access point ARNs and automatic bucket discovery are outside this adapter.

`DefaultConfig(bucket, region)` uses AWS's credential provider chain. Explicit
credentials or a named profile can be supplied. Typed `Config.Endpoint` owns the S3 endpoint;
AWS endpoint environment overrides do not change this scope. `R2Config(bucket, accountEndpoint,
credentials)` requires explicit credentials, HTTPS and the account's R2 endpoint,
uses region `auto` and does not fall back to AWS host credentials. Keep secrets in
an untracked environment/provider configuration, never application source.

Uploads use one reusable `PartBytes` buffer per active upload plus fixed transfer
buffers and a bounded parts manifest. `MaxUploads` bounds uploads across all disks
sharing a backend. Parts are uploaded serially within one object; objects can run
concurrently. Inputs are consumed once. Small/empty inputs use PutObject only
after EOF/length/checksum checks. Larger inputs stage multipart parts and publish
only after complete input validation. Transport Content-MD5 checks each part;
a known full SHA-256 is recorded as metadata. For unknown-length multipart
uploads without an expected checksum, the returned publication has the computed
SHA-256, but a later HEAD may not have a full checksum to return.

Only reads and replayable part uploads/abort cleanup have bounded SDK retries.
PutObject, CreateMultipartUpload and CompleteMultipartUpload never automatically
retry ambiguous effects. Failed multipart work attempts abort with a separate,
bounded cleanup context even when the input context is canceled. Lost creation
responses may not disclose an upload ID; cleanup references still identify the
key scope. Configure provider lifecycle expiration for abandoned multipart work;
`ListUploads` inspects bounded, scoped staging pages. `UploadFromCleanup` recovers
a known upload reference; a lost upload ID requires inspection. `AbortUpload` only
affects that staging upload, verifies remaining parts and performs bounded cleanup
rounds. It rejects keys currently being written by the same adapter. Other-process
owners must be stopped or reconciled first; age alone is not proof of abandonment.
No maintenance call deletes a published object. A cleanup reference is diagnostic
metadata, not authorization, and must remain inside trusted operational tooling.

AWS advertises conditional writes/deletes and immutable version selection. The
R2 profile advertises ranges, conditional reads and conditional single-request
writes. `ConditionalWriteMaxBytes` requires a declared size within `PartBytes` for
conditional writes. `PutBytes`, `PutFile` and `CopyTo` supply their known size
automatically. Unknown-length or larger conditional uploads fail before source
consumption. R2 conditional multipart
completion, conditional deletion and historical versions remain unsupported. AWS
conditional deletion cannot be combined with historical-version selection; the
provider evaluates its condition against the current object. S3's mutable `null`
version is not an immutable VersionID and is rejected as an explicit selector.
R2's upload-generation response header is also not a retained-version selector:
its `ObjectInfo.Version` stays empty and follow-up reads pin the ETag. Supported
provider options must never be silently ignored. Separate live R2 and AWS checks
passed; the owning blueprint records their evidence and tested scope.

## HTTP and links

`storage/http.StoreUpload` persists an already validated `http.UploadedFile`, uses
a server-selected key, closes its temporary reader and returns a stable
`StoredObject`. Existing HTTP multipart limits, spooling, validation and request
cleanup remain the single implementation. Persist uploads before the request
ends; the original upload handle cannot be retained for a later job.

`storage/http.Download(disk, key, options)` returns the existing typed
`http.Download`. It defers opening until the handler/authorization succeeds,
retains HEAD/range/validator handling and closes readers automatically. Seeking
reopens provider ranges pinned to a version or ETag; it does not buffer the whole
object. `Stream` uses the existing finite streaming response without HTTP range
negotiation. Presentation names and declared response media types remain explicit.

Private disks are the default. `disk.PublicURL` requires `Visibility: storage.Public`
and an explicitly configured public base; neither setting changes provider access
policy. An S3 public base is the bucket/CDN root; Namespace is appended from the
same adapter configuration. Keys are escaped literally, including `%`, `?`, `#`,
spaces and Unicode. AWS listing responses decode form-style spaces (`+`)
and escaped literal plus signs (`%2B`) exactly once, including multipart key
markers; R2 retains its path-style listing decoder. An authenticated S3 API endpoint is never guessed to be a
public URL.

`disk.TemporaryURL(ctx, key, storage.LinkOptions{ExpiresIn: 5*time.Minute})` creates
a signed **read** URL for S3/R2. Expiry is bounded by the requested lifetime and
known temporary-credential expiry. Formatting redacts the bearer URL and implicit
JSON serialization fails; explicitly put `link.URL()` in an authorized response
DTO. Revocation can invalidate a link earlier. Local content uses the existing
[typed signed HTTP endpoint](http-signed-urls.md) around the download bridge;
there is no separate local signing algorithm or filesystem server.

## Verification

`testkit/storage.Run` provides the shared adapter contract. The source suite covers
key identity, empty values, interrupted reads, conditional operations, ranges,
listing and cancellation. Local cases add confinement, publication, cleanup,
copy/move, seek and lifecycle behavior. SDK wire cases inject part/completion/abort
failures and check signing/expiry. Native checks passed, including the independent
consumer, five storage compiler-rejection cases and two real-gopls storage probes.
The complete regression includes 810 invalid API cases, 310 editor probes and six
automatic getter/setter field notices. Focused storage/consumer races and 640,168
key/range fuzz inputs also passed. Wire peers are not provider certification;
live R2 verification additionally passed uploads, conditional writes, ranges,
listing, signed/public URL payload integrity, interrupted multipart aborts and
scoped cleanup. Its conditional-delete case skips because that capability is
explicitly unsupported. AWS live verification also passed immutable version reads, conditional deletion,
encoded-key object/multipart pagination, signed payload integrity and cleanup of
historical versions/delete markers. AWS public-URL reads remain an optional test:
the dedicated private bucket keeps public access blocked.

Real-provider tests are opt-in and require explicitly configured test-only buckets
and prefix roots. The framework never creates buckets or changes their policies.
For AWS use `FOUNDRY_TEST_S3_BUCKET`, `FOUNDRY_TEST_S3_REGION`,
`FOUNDRY_TEST_S3_PREFIX` (ending in `/`) and optionally `FOUNDRY_TEST_S3_PROFILE`,
alongside the ordinary private AWS credential chain. R2 uses the equivalent
`FOUNDRY_TEST_R2_BUCKET`, `FOUNDRY_TEST_R2_PREFIX`, `FOUNDRY_TEST_R2_ENDPOINT`,
`FOUNDRY_TEST_R2_ACCESS_KEY_ID`, `FOUNDRY_TEST_R2_SECRET_ACCESS_KEY` and optional
`FOUNDRY_TEST_R2_SESSION_TOKEN`. An explicitly configured `FOUNDRY_TEST_S3_PUBLIC_BASE`
or `FOUNDRY_TEST_R2_PUBLIC_BASE` enables a scoped public-URL read test; it never
changes bucket access policies. Set the corresponding `*_REQUIRED=1` to fail
rather than skip missing configuration. Keep all secrets outside tracked files/logs.

Each test creates a cryptographically random child namespace. It establishes list
and cleanup permissions before writing. Cleanup checks every full key and removes
only test-owned unfinished uploads, objects and (on AWS) historical versions/delete
markers. AWS certification requires version-list/delete permissions so test data
is not orphaned in versioned buckets. `FOUNDRY_TEST_S3_VERSIONED_REQUIRED=1` also
requires the selected test bucket to be already versioned for immutable-version
certification; the tests never enable versioning. Missing credentials or skipped
version coverage remain pending certification, never a passing result.

Run configured cloud cases with `go test -v ./storage/s3 -run '^TestReal'`.
Local resource measurements use `go test ./storage/local -run '^$' -bench
BenchmarkLocalStream -benchmem`; these report throughput/allocations, not a claim
that cumulative allocation equals peak retained memory. Hard input/part/page and
active-operation bounds have separate behavioral cases.
