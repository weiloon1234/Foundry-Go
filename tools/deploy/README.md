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

The release-owned `deploy:jobs-inspect` command supplies the validated inventory.
Restore intake uses a private operation-UUID directory, and the snapshot module
reconciles unstarted restores after exit proof and safety checks. See the
[restore procedure](../../docs/guides/deployment.md#restore-a-redis-snapshot).
Local tests do not establish production cutover or restore readiness.

Run the local tool tests and documentation checks from the worktree root:

```sh
make deploy-tools-check
make docs-check
```

To include the real consumer artisan and Redis boundary, build the fixture and
require integration coverage:

```sh
cutover_artisan_dir=$(mktemp -d)
(cd tests/fixtures/consumer && GOWORK=off go build -o "$cutover_artisan_dir/artisan" ./bootstrap/cmd/artisan)
FOUNDRY_TEST_CUTOVER_ARTISAN="$cutover_artisan_dir/artisan" \
FOUNDRY_TEST_REDIS_ADDR=127.0.0.1:6379 FOUNDRY_TEST_CUTOVER_REQUIRED=1 \
make deploy-tools-check
```

This executes M4's inspection argv against M1's real command and strict parser,
including absent, layout-2, layout-3, blocked and no-Redis inventories. It uses
unique namespaces and deletes only their exact keys. The test removes the Linux
`runuser` wrapper on macOS; it does not exercise systemd. The restore intake test
executes M4 and M3's real identity, inventory binding and archive validation paths;
Redis, age and repository processes use the existing local fakes.

The default suite uses temporary instance state and process, Redis, age and
rclone fakes. The opt-in artisan tests contact only loopback Redis. Container
acceptance remains a separate release check.
