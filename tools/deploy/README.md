# Deployment tools

Applications invoke this bundle through their pinned `go tool foundry deploy`.
The [deployment guide](../../docs/guides/deployment.md) owns operator commands,
confirmation rules, host prerequisites, and recovery procedures.

| Path | Responsibility |
| --- | --- |
| `local/cli.py` | Local release selection, private input, plan review, confirmation, and SSH requests |
| `server/manage.py` | Request validation and registered-instance dispatch |
| `lib/release.py` | Release lifecycle and configuration |
| `lib/upgrades.py` | Immutable bundle installation and protocol negotiation |
| `lib/backup.py` | PostgreSQL and encrypted key/config backups |
| `protocol.json` | Wire protocol and tool generation |

The bundle uses protocol 2.0 and generation 11. Tool upgrades require an installed
host generation of at least 9. The local command checks the installed host's
`hello` response before staging any bundle; the candidate checks again before
planning or installation. Older hosts receive `tools_upgrade_floor`: the host
must already run generation 9 tools before this upgrade. The minimum client
generation remains 1, preserving existing registrations. Generation-9 clients can
still deploy ordinary application releases to generation-11 hosts. Older generation-9
tools reject plans and applies against these hosts, preventing them from bypassing
the retired-bundle ledger.

The bundle rejects deployment attempts containing `result` metadata and backup
receipts without `fresh_at` or journals without `configuration_revision`.
Configuration failures continue to use `outcome: failed_applied`. The release
archive format and protocol 2.0 are unchanged, so the published generation-9
baseline can activate the candidate archive during foreign-upgrade acceptance.

Use `tools-plan`, confirmed `tools-apply`, then an ordinary deploy. Ready instances
receive current unit templates followed by a daemon reload; enablement is preserved
and roles are not restarted by the tool upgrade.

Plans list superseded bundle digests in `prune_bundles`. After selection, registry
publication and journal completion, installation runs the selected bundle's pruning
code with the host lock inherited. Only canonical digest directories are removed;
symlinks and unexpected entries require administrator review. Any incomplete upgrade
journal blocks pruning, including an intent not yet claimed in the registry.
Plans also list completed journals for other bundles in `prune_upgrade_journals`.
`tools-apply`, bootstrap and ordinary deploy return `removed_bundles` and
`removed_upgrade_journals`. Unchanged installs and deploys also prune leftovers.
Before deleting journals, pruning atomically writes their target and previous bundle
digests, excluding the active digest, into the root-owned mode-0600 retired ledger
at the `retired_tools` path in `lib/contracts.py`'s `HOST_PATHS`. The ledger is a
schema-validated, sorted, de-duplicated list. A missing ledger means no recorded
retirements; invalid content fails closed with `invalid_retired_tools`.
The ledger and remaining completed journals prevent retired bundles from being
selected again. Active-bundle journals remain unchanged for operation replay.
After the ledger is durable, pruning atomically renames each obsolete journal into
the root-owned mode-0700 `upgrade_deletions` area declared in `HOST_PATHS`, on the
same filesystem and outside the upgrades directory. Read-only planning checks the
existing deletion area's device, or its parent's device before first use, against
the upgrades directory and rejects incompatible layouts before selection. Pruning
rechecks the requirement. It syncs both parents before
removing the journal's files. Every prune first finishes already queued deletions,
including directories whose intent has been removed. Plans include those operation
IDs; apply and resume do not mistake them for unfinished upgrades. Names, paths and
links are validated before deletion.

Deployment actions include `deploy`, `status`, `rollback`, `restart`, `stop`,
`down`, `up`, `logs`, `shell`, `config`, `keys rotate`, `reconcile`, `report`, `sync`,
`capacity`, `tools-plan`, `tools-apply`, `upgrade-resume`, `backup-configure`,
`backup-run`, `restore-drill`, `backup-keys-rotate`, `backup-recipients-rotate`,
and `dns-token-rotate`.

Before the next deploy, run `ACTION=tools-plan` and apply the reviewed plan with
`ACTION=tools-apply`. Maintenance actions require a release built on Foundry-Go
v0.20.0 or later registering `application.MaintenanceCommands()` with a shared
`maintenance.store`. The tools probe the shared maintenance record through
`maintenance:status` on v0.22.0 and later releases. Lifecycle checks use liveness
while down; `up` clears the pause and verifies readiness. Older releases without
the probe are treated as serving. `ACTION=shell` opens a terminal in the current
release using the saved SSH connection. Install the current generation from [protocol.json](protocol.json) before first use.
`BYPASS=yes` returns a generated bypass path once to the operator without persisting it. See the deployment guide for parameters.

`ACTION=logs [ROLE=http] [LINES=100]` reads recent journal entries for a declared
role. `LINES` must be an integer from 1 to 2000. Snapshots exceeding the 4 MiB JSON
limit are rejected; reduce `LINES` or add `FOLLOW=yes` to stream directly over SSH.
Follow mode uses the saved target and key, strict host-key checking, and root or
passwordless `sudo -n`. It allows interactive authentication without requesting a
remote terminal, including when stdout is a terminal. Ctrl-C ends the stream quietly; SSH
connection failures still fail. `FOLLOW` accepts only `yes` when supplied, and
other actions reject both variables. These options are included in generation 8
for v0.22.0; install the updated bundle for snapshot `LINES` support.

Run the local tests and documentation checks from the worktree root:

```sh
python3 -B -m unittest discover -s tools/deploy/tests
bash -n tools/deploy/tests/run_acceptance.sh tools/deploy/tests/run_ci_acceptance.sh
make docs-check
```

The default suite uses temporary instance state and process/service fakes.
Real-systemd contracts are opt-in locally and run in framework CI.
The same [Verify and security workflow](../../.github/workflows/verify.yml) runs
container acceptance on native amd64 Ubuntu 24.04 runners for pushes and manual
dispatches. Its four cells cover Ubuntu 24.04 and 26.04, each with `--fresh` and
`--foreign --upgrade`. Pull requests and weekly schedules skip this matrix.
Each cell builds one archive, has a 60-minute timeout, and runs independently
with fail-fast disabled. Failure artifacts retain only top-level `.log`, `.txt`,
and `.out` files for 14 days. Private files and directories are excluded, and
the runner still cleans up its containers, volumes, and private material.

The CI entry point requires a new log directory and one OS/mode pair:

```sh
ACCEPTANCE_LOG_DIR="$RUNNER_TEMP/deploy-acceptance-logs" \
  make deploy-container-acceptance-amd64 DEPLOY_ACCEPTANCE_UBUNTU=24.04 \
  DEPLOY_ACCEPTANCE_MODE=foreign-upgrade
```

The [acceptance fixture](../../tests/fixtures/deploy-acceptance/) supplies the archive
for every phase. Its checked-in module uses `v0.0.0` with a local replacement for
native tooling. `build_acceptance.py` uses the existing release packager to publish
this checkout into a private file proxy, copies the fixture through the deploy source
allowlist, and drops the replacement only in that copy. The copy requires the unique
candidate version and retains the original Git provenance; the archive digest binds
its rewritten source bytes. Workspaces remain beneath the repository's ignored
`.cache/deploy-acceptance/` directory, including after a failed Docker build.
Only the acceptance build container receives the read-only proxy mount and Go proxy
environment. Only the framework module bypasses the checksum database; public
modules still use `sum.golang.org`. The production builder continues to reject local
replacements and has no proxy configuration option.
On a native arm64 Docker host, the build step can run independently of host scenarios:

```sh
framework="$PWD"
build_info=$(mktemp -d)
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 \
  --archive-file "$build_info/archive.txt"
archive=$(cat "$build_info/archive.txt")
tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --fresh
```

The opt-in `--upgrade` phase tests the published generation-9 baseline selected by
`baseline_version` in `tests/run_ci_acceptance.sh`, independently of the fixture's
candidate framework. The CI entry point above resolves that tag and passes its
unmodified module-cache tools through `--tools-root`. The same candidate application
archive serves both baseline and candidate tools. When their bundle digests match,
the phase asserts an unchanged result and registry; otherwise it asserts replacement.
The foreign-service isolation checks run in either case.

The runner bootstraps and deploys using the published bundle, upgrades through the
candidate's `tools-plan`/`tools-apply`, then deploys again. It proves the active
bundle matches the candidate and excludes removed files, role files and loaded
units have no `ExecStartPre`, roles are healthy, and the report is clean except
for the same container-only failed units tolerated by the host phase. The host,
role, backup, edge, and replacement-host recovery phases then run normally.
Omitting `--upgrade` runs those phases directly.

Phase diagnostics identify artisan calls visible in an exception traceback.
Only a failed `config:check` is re-invoked (read-only, ten-second limit); its stderr
sample is limited to 4 KiB/eight lines with only fixed diagnostic phrases exposed.
No failed migration, seed, restore, or other mutation is replayed.

See [container acceptance](../../docs/guides/deployment.md#container-acceptance)
for retained phase logs and the remaining external-service acceptance requirements.

Generation 11 adds the opt-in upload profile in `lib/uploads.py` and
`server/templates/nginx-upload.conf`. Install it through reviewed `tools-plan`
and `tools-apply`. Bootstrap validates administrator-provisioned, independently
sized upload volumes, creates private service directories, and renders the
runtime values, service write paths, and HTTP/HTTPS upload locations together.
Existing profiles without `uploads` keep their proxy behavior. See
[large-upload ingress](../../docs/guides/deployment.md#large-upload-ingress).

The integrated runner also executes `tests/acceptance_uploads.py` after edge
acceptance. It uses the existing fixture, rendered nginx and service sandbox,
150 seconds of silent storage publication, slow clients and dedicated quota
volumes. See the [B26 evidence requirements](../../docs/evidence/b26-resource-provider-acceptance.md).
Resource and live R2 certification remain separate lead-run gates.
