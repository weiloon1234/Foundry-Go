# Compatibility and rollout policy

This policy accompanies the first complete Foundry-Go implementation and its
[local candidate acceptance](production-acceptance.md). Rust supplies the feature
reference; Go source names and diagnostics schemas are native contracts, not
byte-for-byte Rust API compatibility. No public version has been published by
this acceptance work.

## Modules and public Go APIs

Root `go.mod` owns the supported Go requirement. All fixtures and development
modules use that requirement; release tools read it rather than pinning a second
SDK. Public Go packages, generated consumer APIs and observable behavior form the
release surface. `internal` packages are implementation details. Exported structs
should be initialized by named fields or provided constructors so additive fields
do not force positional-literal changes.

Before 1.0, a minor release may contain documented breaking changes; a patch
release preserves supported public signatures and wire contracts except an
explicitly documented security correction. Once a stable major is declared,
breaking source changes require the corresponding Go major module path and a
migration guide. This document does not choose a tag or publish any version.

Pin the framework version in each application and plugin. Run generation with
that same version and keep generated outputs together with their ownership
manifests. Changes to generated binary ownership, field capabilities, query
scopes or method signatures require regeneration and consumer compilation. A
successful runtime codec test alone does not establish generated-model support.
Do not combine a newer generator with an older runtime by accident.

Plugin release versions and framework compatibility requirements use the existing
typed [plugin manifest](../plugin/manifest/manifest.go). Its `FrameworkVersion`
identifies the plugin API contract independently of Go's SDK version or an
application payload version. Change it deliberately when that compatibility
surface changes and verify base/dependent/consumer resolution together. It is
not inferred from a synthetic packaging candidate version.

## Persisted and transport formats

| Contract | Rollout rule |
| --- | --- |
| Generator ownership/journal format | Use its existing versioned decoder and recovery rules. Keep manifests with owned output; never overwrite unknown ownership. |
| Public contract manifest/OpenAPI/TypeScript | Export from the registered descriptors, regenerate adapters together, and validate old/new clients against the server. Strict decoders can reject additive fields. |
| WebSocket protocol | Retain the declared protocol version and frozen transport fixtures. A version change requires explicit negotiation/migration; do not silently reinterpret messages. |
| Job payload `Version` | Keep handlers for retained producer versions until queued, delayed, retried and workflow work has drained or been deliberately migrated. |
| Job envelope transport version | Legacy shape remains the default. Enable optional trace format 2 only after every worker/outbox/workflow reader can decode it. |
| Outbox records | Preserve captured bytes, IDs and destinations across retries. An ambiguous publish is reconciled using the same captured pending operation. |
| Application/database schema | Use reviewed forward migrations with compatible expansion/backfill before contraction. No automatic destructive synchronization or reset. |

See [job trace rollout](guides/job-trace-rollout.md),
[client contracts](guides/client-contracts.md), [realtime](guides/websocket.md),
[plugins](guides/plugins.md) and [outbox](guides/outbox.md) for the actual typed
contracts and limits. Existing application-specific payload migrations remain the
application's responsibility; a framework upgrade cannot infer their meaning.

Adding a field, changing optional/null behavior, changing numeric representation,
renaming an operation or tightening validation can break a strict peer. Test both
directions with retained old fixtures before calling a change compatible. Keep
all readers compatible with retained data before enabling new writers. Rollback
must preserve a reader capable of consuming formats already written.

## Operational behavior

Default-safe opt-ins remain explicit: incoming trusted trace context, outgoing
HTTP propagation, queued trace transport and read-pool routing. Replica reads do
not promise immediate visibility after primary commits. Use the primary executor
or actual transaction when the operation requires that visibility.

Maintenance stops admission while existing owners finish. A deadline bounds the
caller's wait; it does not prove a callback stopped or release its resources.
Do not turn a timeout into silent data loss or close a dependency under admitted
work. Use lifecycle state, owner `Done` signals and bounded diagnostics during
rolling termination. Operational additions preserve ordinary authentication,
permissions, redaction and typed identifiers.
