# Redis cutover recovery module

[redis_snapshot.py](lib/redis_snapshot.py) implements the M3 module boundary.
Server and local action dispatch belong to M4. Startup fencing, process ownership,
and verified drain belong to M2. This module does not change application state,
start roles, clear compatibility protection, or invoke queue migration.

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
A missing M2 boundary fails closed with `operation_locked`. Exceptions from an
installed boundary propagate unchanged.

Scopes contain the M1 storage identity, literal job prefix, and exact allowed
physical keys. The module checks the Foundry Redis preparation journal, persisted
and live instance ACLs, and other registered instances' grants before data access.
Only the managed local TLS allocation and database zero are accepted. A shared
Redis service can contain other applications' namespaces; the approved job scopes
must belong exclusively to this instance. Application ACLs gain no privileges.

Capture uses cursor-complete SCAN and atomic Lua DUMP plus PEXPIRETIME. Unknown
keys, incomplete scans, foreign ownership, and exhausted limits fail closed.
The module consumes physical keys from M1; it does not reconstruct job key names.
A proven no-Redis operation skips capture in M4.

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

The restore identity file is an absolute, root-owned mode-0600 file beneath the
instance private directory, within a directory component named by the operation
UUID. For example, M4 can place it at `intake/<operation UUID>/identity`.
The identity contents are never passed in process arguments. The private plan
and public result omit the identity path and contents. M4 owns intake cleanup.

Restore planning downloads and checks the ciphertext, decrypts privately, and
validates every archive member, DUMP checksum, and serialized Redis value before
returning a plan. The installed `redis-check-rdb` performs offline value validation;
it opens no Redis connection. Its input is a private temporary RDB assembled from
the opaque DUMP values. The native validator and age decryption have process or
file limits through `prlimit`, with bounded command deadlines.

The plan binds the archive, exact deletion inventory, identities, and scope.
Its warning is `restore_may_replay_external_side_effects`. Apply requires literal
`confirm: true`, the plan token, and a complete safety snapshot from the same
restore operation after planning. M4 captures that safety snapshot after verified
drain. New keys or missing unexpired keys invalidate an unstarted plan. Natural
expiration is allowed and never extends a key's original lifetime.

Each RESTORE or exact-key DEL has durable dispatch intent before execution.
A failed write remains uncertain. Only explicit same-intent `resume_restore` can
retry after quiescence proof. RESTORE uses absolute expiration and takes an atomic
post-write observation. Final verification compares that representation and its
expiration; Redis may change a hash or set's serialized ordering while loading.
Only the plan's exact extra keys are deleted. No FLUSH command is used.

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

The affected suites run with:

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_redis_snapshot test_backup test_redis_acl
```

The complete deployment-tool check is `GOWORK=off make deploy-tools-check`.
