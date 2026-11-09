# Redis cutover recovery module

[redis_snapshot.py](lib/redis_snapshot.py) owns scoped capture and restore.
[release.py](lib/release.py) coordinates the operation through the server and local
`redis-restore-plan`, `redis-restore-apply`, and `redis-restore-resume` actions.
The [operator procedure](../../docs/guides/deployment.md#restore-a-redis-snapshot)
owns plan review, private identity intake, confirmation, and resumption.
Startup admission, process ownership, and verified drain remain with the
coordinator and [compatibility guard](lib/compatibility.py). This module does not
change application state, start roles, clear compatibility protection, or invoke
queue migration.

The existing [backup pipeline](lib/backup.py) owns recipients, private repository
settings, age encryption, immutable rclone upload, and downloaded-byte verification.
PostgreSQL backup behavior remains unchanged. Redis artifacts use the same bucket
at `redis/<instance UUID>/<snapshot UUID>.json.age`.

## Coordinator contract

The four synchronous functions take `(host, registry, entry, request)` and return
the schema-1 result defined by the cutover integration contract. Unknown request
fields and booleans in integer fields are rejected.

| Function | Required request fields | Result |
| --- | --- | --- |
| `capture` | `schema`, `operation_id`, `snapshot_id`, `source_release`, `configuration_revision`, `inventory_sha256`, `scopes` | `SnapshotEvidence` |
| `plan_restore` | `schema`, `operation_id`, `snapshot_id`, `inventory_sha256`, `scopes`, `identity_file` | `RestorePlan` |
| `apply_restore` | `schema`, `operation_id`, `plan_token`, `confirm`, `safety_snapshot` | `RestoreEvidence` |
| `resume_restore` | Same fields as `apply_restore`, with the original accepted values | `RestoreEvidence` |

The caller holds host and instance locks, in that order, and proves previous
executor children have exited before retrying. `capture`, `apply_restore`, and
`resume_restore` independently call
`release.require_cutover_quiescence(host, entry, operation_id)`.
A missing quiescence boundary fails closed with `operation_locked`. Exceptions
from an installed boundary propagate unchanged.

Scopes contain the release-owned storage identity, literal job prefix, and exact
allowed physical keys. The module checks the Foundry Redis preparation journal, persisted
and live instance ACLs, and other registered instances' grants before data access.
Only the managed local TLS allocation and database zero are accepted. A shared
Redis service can contain other applications' namespaces; the approved job scopes
must belong exclusively to this instance. Application ACLs gain no privileges.

Capture uses cursor-complete SCAN and atomic Lua DUMP plus PEXPIRETIME. Unknown
keys, incomplete scans, foreign ownership, and exhausted limits fail closed.
The module consumes physical keys from the `deploy:jobs-inspect` contract; it does
not reconstruct job key names. SCAN patterns, physical keys, and serialized values
travel in a JSON request on stdin through the existing private Redis admin boundary.
Process arguments contain fixed Lua scripts, with no physical key names or payloads
embedded in them. A proven no-Redis operation skips capture in the coordinator.

The coordinator stores complete capture evidence at `state.cutover.snapshot`
before migration dispatch. Evidence binds the snapshot and operation UUIDs,
host, project, and instance identities, source release, configuration revision, inventory
and scope digests, capture and completion times, key count, encrypted byte count,
ciphertext checksum, and object key. A restore's fresh safety capture is stored at
`state.cutover.safety_snapshot`; completion is stored at `state.cutover.restore`.
The [evidence reference](../../docs/guides/deployment.md#redis-snapshot-evidence)
lists the public field names. Freshness means capture inside the operation window.

## Private records and recovery

Records live beneath the existing private instance backup directory:

```text
backups/redis/
  snapshots/<snapshot UUID>/
    journal.json
    <snapshot UUID>.json.age
    receipt.json
  restores/<operation UUID>/
    intent.json
    archive.json
    plan.json
    progress.json
    write-intent.json
    receipt.json
```

Directories have mode 0700 and files have mode 0600. Root owns production records.
`archive.json` in a snapshot directory exists only during capture and encryption.
It is deleted before upload. The journal binds the original archive, recipients,
and repository destination. Credentials are read through the existing private
repository boundary and never enter evidence.

A completed capture returns its immutable receipt for the same intent. Reusing
the snapshot ID with a different intent fails with `operation_conflict`.
An interrupted initial capture cannot be silently repeated. Once capture is
complete, retries preserve that archive and its ciphertext. An uncertain upload
first checks the remote bytes, then can retry the retained artifact with
`--immutable`. Only downloaded-byte verification permits a receipt. Destination
changes, missing capture records, and altered artifacts require reconciliation.

The restore identity file is an absolute, root-owned mode-0600 file at
`<instance private>/operations/<operation UUID>/restore.identity`.
The UUID directory must match the request's operation, be root-owned, and have
mode 0700. Other filenames, directory layouts, UUIDs, and symlinks are rejected.
The identity contents are never passed in process arguments. The private plan
and public result omit the identity path and contents. The coordinator owns
intake cleanup.

The coordinator saves each snapshot's validated inventory and evidence in an
immutable private binding. Restore planning uses that binding even when current
values fail migration eligibility. Both ownership and exact-key coverage remain
mandatory, and the returned snapshot evidence must match the binding.

Restore planning downloads and checks the ciphertext, decrypts privately, and
validates every archive member, DUMP checksum, and serialized Redis value before
returning a plan. The installed `redis-check-rdb` performs offline value validation;
it opens no Redis connection. Its input is a private temporary RDB assembled from
the opaque DUMP values. The native validator and age decryption have process or
file limits through `prlimit`, with bounded command deadlines.

The plan binds the archive, exact deletion inventory, identities, and scope.
Its warning is `restore_may_replay_external_side_effects`. Apply requires literal
`confirm: true`, the plan token, and a complete safety snapshot from the same
restore operation after planning. The coordinator captures that safety snapshot
after verified drain. New keys or missing unexpired keys invalidate an unstarted plan. Natural
expiration is allowed and never extends a key's original lifetime.

Each RESTORE or exact-key DEL has durable dispatch intent before execution.
A separate `write-intent.json` binds the accepted request before the first Redis
write and remains after completion. If M4 records dispatch but M3 never publishes
`progress.json`, same-intent resume can proceed after exit proof and all existing
plan, safety-snapshot, ownership, and drift checks. A write marker or receipt with
missing progress requires reconciliation; it cannot be treated as unstarted.
Unreadable or corrupt progress also fails closed.
A failed write remains uncertain. Only explicit same-intent `resume_restore` can
retry after quiescence proof. RESTORE uses absolute expiration and takes an atomic
post-write observation. Final verification compares that representation and its
expiration; Redis may change a hash or set's serialized ordering while loading.
Only the plan's exact extra keys are deleted. No FLUSH command is used.

The coordinator records dispatch before calling the module. After that point,
it calls `resume_restore` on every retry, even if interruption preceded the module's
initial progress write. The module owns reconciliation of that unstarted intent.
The snapshot lane's corresponding reconciliation change must be integrated and
verified; the coordinator never skips the module's plan, safety-snapshot, or drift checks.

Success and failure both retain the startup fence. Restored layout-2 queues still
require a reviewed forward cutover before candidate startup. Redis recovery does
not reconcile database changes or external side effects.

## Bounds and tests

The module accepts at most 64 scopes, 262,144 allowed keys, and 4 MiB of scope JSON.
A key is limited to 4,096 UTF-8 bytes, a DUMP to 1 MiB, and the archive to 64 MiB.
SCAN has a shared budget of 10,000 calls and 120 seconds. Exceeding a bound fails;
no partial archive satisfies the snapshot prerequisite.

[The snapshot tests](tests/test_redis_snapshot.py) use the existing Host fixture
with private temporary files and Redis, age, and rclone fakes. They cover ownership,
checksums, bounded discovery, upload reconciliation, confirmation, expiration,
archive validation, exact deletion, and partial restore. No test contacts a server.
They also cover the exact M4 identity path, private file and directory permissions,
the dispatch gap before initial progress, missing progress after a possible write,
and the actual Redis admin argv/stdin boundary for capture and restore.

The affected suites run with:

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_redis_snapshot test_backup test_redis_acl
```

The complete deployment-tool check is `GOWORK=off make deploy-tools-check`.
