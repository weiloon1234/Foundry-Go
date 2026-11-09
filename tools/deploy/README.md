# Deployment tools

Applications invoke this bundle through their pinned `go tool foundry deploy`.
The [deployment guide](../../docs/guides/deployment.md) owns operator commands,
confirmation rules, host prerequisites, and recovery procedures.

| Path | Responsibility |
| --- | --- |
| `local/cli.py` | Local release selection, private input, plan review, confirmation, and SSH requests |
| `server/manage.py` | Request validation and registered-instance dispatch |
| `lib/release.py` | Release lifecycle and durable cutover and restore coordination |
| `lib/compatibility.py`, `server/start_check.py` | Persistent queue compatibility and startup admission |
| `lib/cutover_contracts.py` | Release-owned inventory validation, storage coverage, and private intent records |
| `server/cutover_runner.py` | Pinned systemd executor that survives SSH disconnects |
| `lib/redis_snapshot.py` | Scoped Redis capture, verified encrypted upload, and explicit restore |
| `lib/backup.py` | Shared backup repository, age recipients, encryption, and upload verification |
| `protocol.json` | Wire protocol and tool generation |

Generation 5 installs role startup guards and adds `cutover-plan`, `cutover-apply`,
`cutover-resume`, `redis-restore-plan`, `redis-restore-apply`, and
`redis-restore-resume`. Apply and resume require the reviewed plan token, the same
operation UUID, and confirmation. They cannot start old code after a migration
may have run. A confirmed stop can contain processes while retaining the gate.
`cutover-forward-plan` and confirmed `cutover-forward-apply` bind a replacement
artifact to an unresolved operation. They retain the floor and recovery evidence.
The replacement operation resumes through `cutover-resume`.

The [cutover procedure](../../docs/guides/deployment.md#coordinated-redis-queue-cutover)
describes full downtime, the owner's client-coverage attestation, fail-closed queue
discovery, snapshot evidence, failure handling, and resumption. The
[snapshot module reference](redis-snapshot.md) describes exact-key ownership,
private journals, expiration, and restore limits.

The release-owned `deploy:jobs-inspect` implementation and the snapshot lane's
unstarted-restore reconciliation changes still need integration and verification.
Restore intake uses a private operation-UUID directory. See the
[restore procedure](../../docs/guides/deployment.md#restore-a-redis-snapshot).
Passing fake-based tests does not establish production cutover or restore readiness.

Run the local tool tests and documentation checks from the worktree root:

```sh
make deploy-tools-check
make docs-check
```

Tests use temporary instance state and process, Redis, age, and rclone fakes.
They do not contact a real server. Container acceptance and merged-command
integration remain separate release checks.
