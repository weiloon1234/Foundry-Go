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

The bundle uses protocol 2.0 and generation 12. Tool upgrades require an installed
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

### Generation-12 feature preview

This checkout contains the preview owners but does not yet activate features:
shared registry/profile/topology reader dispatch still rejects the feature schemas,
and lane C's network integration is absent. Both integration gates must be filled
before running the end-to-end scenario below; no frozen declaration changes are needed.

The preview uses the frozen topology-2, profile-3 and archive-2 contracts. Single
instance custom services receive typed argv, hardened units, local probes,
per-start configuration and protected bundle views. Wrappers, counts above one
and ordinals other than one remain release-2 capability refusals. Legacy apps and
generation 9, 10 and 11 hosts retain their existing paths.

`feature_lifecycle.py` owns provision/deploy/restart/stop orchestration. Provision
passes declared package requirements to lane B's reviewed transaction. Deploy
verifies and publishes lane B artifacts before building frozen `Deployment`,
`UnitSpec` and `RuntimeResolution` records. Pending admission precedes stops,
migrations and selection. Validation starts run in dependency/readiness order with
one allowance per unit and a live operation executor. Completion commits admission.
A failed transition remains pending for reconciliation; it never rolls back code
or credentials automatically.

HTTP allocation is a journaled runtime selection. Every role gets an immutable,
root-owned mode-0640 effective environment derived from current private values.
Units, console commands and runtime descriptors select that same resolution.
Environment hashes stay private. Automatic restart reuses the exact selected
resolution; it never reallocates a port. The boot coordinator proves closure and
starts the enabled selection in readiness order before requesting public opening.

Application code calls `deployment.LoadBindings()` once at assembly. Its result
is an immutable snapshot with typed `Executable(name)`, `Endpoint(name)` and
`Bundle(name)` lookups; missing names return `fault.Missing`. The explicit
`FOUNDRY_DEPLOY_RUNTIME_FILE` selects a schema-2 role descriptor containing the
frozen schema-1 `RoleBindings`. An invalid explicit descriptor is an error, never
an empty/default binding set. `deployment.Detect()` uses that same descriptor.
The app-facing declarations and loader do not change when later tool hardening ships.

The preview explicitly refuses with `capability_unavailable` naming the later
release:

- Feature rollback, including feature-to-legacy rollback.
- Configuration changes, key rotation, configuration/maintenance holds and `up`.
- Named lifecycle operations and interrupted-operation reconciliation/recovery.
- Custom-service secret intake and bundle ABIs without package-version evidence.
- Helper-bundle pruning: automatic retention returns a capability diagnostic and
  retains every bundle and upgrade journal. Invocation/view bounds still apply;
  reaching a bound refuses further starts until proven-exit cleanup ships.
- Host reports/capacity operations involving features until their owners integrate.

Preview `down` closes ingress and stops/disables the whole instance. It is a
permanent stop in this preview, not a recoverable maintenance hold. Public opening
is refused before host mutation unless lane C implements all three functions in
`listeners.py`, reached only through `lib/feature_network.py`:

| Function | Handoff |
| --- | --- |
| `feature_allocate(host, entry, topology, profile, operation_id)` | No effects; returns allocated endpoints and nginx projection SHA-256 |
| `feature_close(host, entry, transition, resolution, units)` | Returns observed, complete `ClosureEvidence` covering exact units and endpoints |
| `feature_open(host, entry, resolution, evidence)` | Opens only the validated selection and returns `True` after verification |

No caller fabricates closure when that integration is missing. The fixed
`runtime-v1` entry point pins an invocation to an immutable tool bundle before
importing its helper. The operation executor runs independently of its SSH
observer. Independent executor cleanup and selective retention remain later work.

The opt-in real-systemd scenario uses a newly built ordinary deploy-acceptance
archive. It adds one artifact-backed relay and a declared package, verifies the
HTTP app reaches the relay through the Go binding loader, restarts the instance,
and verifies maintenance stops it. After lane C is merged, run:

```sh
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 --archive-file /tmp/b24-archive-path
bash tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive "$(cat /tmp/b24-archive-path)" --framework-root "$PWD" --fresh --feature-preview
```

Use matching `amd64`/`linux/amd64` arguments for that architecture. This is a
container-only acceptance scenario; ordinary Python tests never invoke it.
