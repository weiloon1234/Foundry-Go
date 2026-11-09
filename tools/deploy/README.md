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
`cutover-drain-resolve` records a confirmed reviewer decision against exact stopped
process evidence and the failed or uncertain candidate startup attempt. Every
candidate startup failure requires this review, even if a later retry was refused
by the guard. Missing history from migration dispatch onward and malformed history
also require review; clean containment cannot clear the uncertainty. `stop` records
that requirement, so `cutover-drain-resolve` is available directly afterward.
The guard-refusal exemption applies only after reboot following a
completed drain, before any candidate attempt, including `start-limit-hit` with
zero main-process evidence. `cutover-abort-plan` and confirmed `cutover-abort-apply` replace
a pre-dispatch operation with a corrected inventory while keeping startup fenced.
Both procedures are described in the deployment guide.

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

When `redis-server`, `redis-cli`, `redis-check-rdb`, `age` and `age-keygen` are
available, the suite also runs a real local M4/M3 capture, restore and lost-response
resume against existing loopback Redis (`FOUNDRY_TEST_REDIS_ADDR`, default
`127.0.0.1:6379`). It creates unique keys and deletes only those keys. Missing tools
skip this test; a configured local service failure fails it. No Redis server is
started. Encryption, DUMP/RESTORE, absolute expiry and RDB validation run for real;
systemd/managed ACL observations, Linux `prlimit`, and remote object transport use
local fixture adapters. The repository adapter stores and compares real ciphertext
in a temporary directory. This does not verify a production repository or systemd.

The dedicated [deploy acceptance fixture](../../tests/fixtures/deploy-acceptance/)
supplies the archive for every container phase. Build it with the normal deploy
builder, then run both arm64 cells on a native arm64 Docker host:

```sh
framework="$PWD"
build_info=$(mktemp -d)
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 \
  --archive-file "$build_info/archive.txt"
archive=$(cat "$build_info/archive.txt")
tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --foreign --recovery
tools/deploy/tests/run_acceptance.sh --ubuntu 26.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --foreign --recovery
```

The helper uses `local/cli.py`'s builder with the fixture's acceptance profile and
published framework pin; it installs nothing on a host. Docker builds the Linux
archive once, and both cells consume that archive. Without `--recovery`, the
existing phases run unchanged and can use the same fixture archive.

`--recovery` exports `v0.15.0:tools/deploy` from the local Git tag into the private
run logs and mounts it read-only. A preparation phase bootstraps and activates only
the first instance with that historical bundle. The incident phase proves ordinary
queue-gated stop and cutover stop-proof refusals on layout 3, upgrades through current
`tools-plan/tools-apply`, proves abort refusal, recovers forward, and verifies stop
and restart. Only then does the full host phase run on current tools with
`--after-incident`: it verifies the prepared topology and age recipient, reuses
the identity, and leaves the existing instance's acknowledged bootstrap secrets
unchanged. Fresh runs retain exclusive creation. After backup and edge acceptance,
the fast phase covers two-queue drain with work in flight, forced worker death
and drain review, readiness failure and forward recovery, exact Redis snapshot
restore, and containment of held and completed releases. Readiness variants are
immutable test-side archive repacks with updated manifests, using the same built
binary. No installed tool bundle is patched.

After successful incident recovery, the test records the superseded executor's
`failed`/`exit-code`/`ExecMainStatus=1` evidence and resets only that exact unit.
Production recovery currently retains this systemd failure record; the acceptance
report still rejects every unexpected failed unit and prints its result/status.

Phase diagnostics identify artisan calls visible in an exception traceback.
Only a failed `config:check` is re-invoked (read-only, ten-second limit); its stderr
sample is limited to 4 KiB/eight lines with only fixed diagnostic phrases exposed.
Subprocess-only failures without that traceback retain the ordinary state/journal
diagnostics; no failed migration, seed, restore or other mutation is replayed.

Container evidence, retained phase logs and the deferred requirements are described
under [container acceptance](../../docs/guides/deployment.md#container-acceptance)
and the [full cutover matrix](../../docs/guides/deployment.md#full-matrix-required-before-the-next-queue-layout-change-ships).
