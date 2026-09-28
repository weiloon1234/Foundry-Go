# Storage adapters and object ownership

These instructions add to the [repository rules](../AGENTS.md). The
[storage guide](../docs/guides/storage.md) owns operation, capability and outcome
contracts. Extend the existing Disk/Backend and consumer paths.

## Keys, confinement and provider capabilities

- Preserve exact validated object-key identity. An object key, prefix, client filename
  or URL is not a filesystem path. Do not clean, decode or normalize it into a different
  object. Keep provider-specific key restrictions explicit and reject unsupported
  names before I/O.
- Preserve the local managed format, root confinement and publication locking.
  Reject unrelated populated roots; never import, overwrite or clean arbitrary
  directories. Keep staging and cleanup within the adapter's owned root.
- Validate capabilities before consuming input or mutating objects. Unsupported
  conditions/version selectors must fail explicitly, never be silently ignored or
  emulated with a racy check-then-write. Preserve actual AWS/R2 differences; ETags and
  upload-generation headers are not retained immutable version IDs.
- Cursors, object identities and cleanup references grant no authorization. Preserve
  server-selected namespaces/keys and authorize before opening or issuing a link.
  Public URL configuration does not change provider access policy; signed bearer
  links require explicit authorized disclosure.

## Transfers, outcomes and cleanup

- Keep copy/move source and destination outcomes distinct. Move conditionally deletes
  the original and preserves concurrent replacements; it is not an atomic rename.
  A failed source deletion must not trigger deletion of the published destination.
  Use full store/object identity to detect aliases across disk names.
- Retain established mutation outcomes and cleanup references through cancellation
  and error-classification failures. Applied-with-error remains applied; unknown
  publication requires reconciliation. Do not automatically retry ambiguous object
  publication or multipart creation/completion.
- Failed multipart work must attempt abort using the existing separately bounded
  cleanup context. Maintenance affects only owned staging uploads, never published
  objects. A lost upload ID requires inspection; age alone does not prove abandonment.
- Preserve borrowed input versus owned output-reader lifetimes. Drain disks before
  closing adapters; a timed-out wait does not release a still-running operation.
  Stream large objects and pin reopened ranges to their version/validator, rather
  than buffering the whole object or mixing different versions.
- Reuse the [HTTP bridge](../docs/guides/storage.md#http-and-links) and read
  [http/AGENTS.md](../http/AGENTS.md) for transport changes. Persist request uploads
  before request cleanup; pass stable stored-object identities to jobs, never a
  request-owned temporary upload handle.

## Evidence to select for the completed batch

Reuse [the adapter contract](../testkit/storage/contract.go). Select confinement,
key identity, conditional replacement, copy/move, interrupted stream, publication
and multipart-abort cases for changed paths; assert stored bytes and cleanup state.
Use relevant races/fuzzing and independent consumer/type checks after the full batch.
SDK wire tests do not certify a live provider. Live tests require configured test-only
buckets/prefixes and scoped cleanup; never create buckets or change access policies.
