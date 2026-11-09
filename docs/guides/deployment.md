# Deployment operator contract

Foundry-Go owns the Python deployment tooling under `tools/deploy`. Applications
own their identity, profiles, topology, and nonsecret application configuration.
The bundle implements layout v2, dedicated shared services, role lifecycle,
Cloudflare certificates, and off-host recovery. Container acceptance remains a
release requirement; unit tests do not establish live package or provider behavior.

<a id="agent-entry"></a>
## Agent entry

- [Invoke pinned deployment tools](#recipe-deploy)
- [Coordinate a Redis queue cutover](#recipe-queue-cutover)
- [Probe release storage](#recipe-storage-probe)
- [Prepare readiness](production-operations.md#recipe-startup-readiness)

<a id="recipe-deploy"></a>
## Invocation

`go tool foundry deploy` runs the Python tool from the application's pinned
Foundry-Go module and refuses a known tool/framework version mismatch. It passes
`--dir`, which defaults to the current directory, to the Python tool as the
application directory. An application Makefile can contain these thin targets:

```makefile
.PHONY: bootstrap deploy release
bootstrap deploy release:
	GOWORK=off go tool foundry deploy --dir "$(CURDIR)" "$@"
```

The operator's machine needs Python 3.11 or later. Deployment rules stay in the
Python tool. The application directory must contain `go.mod`. Framework tools
remain in the selected module, and local state remains in the application directory.

| Command or action | Purpose |
| --- | --- |
| `make bootstrap` | Select or create an environment, then inspect or plan preparation |
| `ACTION=inspect make bootstrap` | Inspect public identity and host state |
| `ACTION=recover make bootstrap` | Recover an existing instance onto an empty replacement server |
| `make release` | Build a local release archive |
| `ENVIRONMENT=production ACTION=status make deploy` | Read the selected instance status |
| `ACTION=deploy make deploy` | Select or build a release, then request activation |
| `ACTION=config make deploy` | Submit up to 16 related configuration values together |
| `ACTION=report make deploy` | Read the public host report without sudo |
| `ACTION=sync make deploy` | Show the local profile and topology diff against the registry |
| `ACTION=capacity make deploy` | Review and apply RAM-based PostgreSQL and Redis sizing |
| `ACTION=tools-plan make deploy` | Stage the selected framework bundle and inspect an upgrade plan |
| `ACTION=tools-apply make deploy` | Apply the reviewed tool plan with `PLAN_TOKEN` and `OPERATION` |
| `ACTION=upgrade-resume make deploy` | Resume that same tool operation after interruption |
| `ACTION=cutover-plan make deploy` | Stage a release and review the full-downtime queue cutover |
| `ACTION=cutover-apply make deploy` | Start the reviewed cutover with `PLAN_TOKEN` and `OPERATION` |
| `ACTION=cutover-resume make deploy` | Resume the same cutover after inspecting its recorded outcome |
| `ACTION=cutover-forward-plan make deploy` | Bind a replacement release to an unresolved layout-3 operation |
| `ACTION=cutover-forward-apply make deploy` | Confirm forward recovery with its reviewed token and a new operation UUID |
| `ACTION=redis-restore-plan make deploy` | Review an instance snapshot selected by `SNAPSHOT`, using a private age identity |
| `ACTION=redis-restore-apply make deploy` | Confirm the reviewed Redis restore with `PLAN_TOKEN` and `OPERATION` |
| `ACTION=redis-restore-resume make deploy` | Resume the same restore while keeping application startup blocked |
| `ACTION=backup-configure make deploy` | Reconcile the persisted backup binding and schedules |
| `ACTION=backup-run BACKUP_KIND=full make deploy` | Complete a database and encrypted key/config backup |
| `ACTION=restore-drill BACKUP_ID=<UUID> make deploy` | Restore an isolated PostgreSQL recovery target |

`ENVIRONMENT` selects an existing profile. `RELEASE` selects an exact retained
release version, or `build`. Interactive operations request confirmation after
showing their plan. `CONFIRM=yes` supplies that confirmation in automation.
Tool upgrades, cutover application, and Redis restore application additionally
require the reviewed `PLAN_TOKEN` and a stable UUIDv4 in `OPERATION`.
The [cutover prerequisites](#coordinated-redis-queue-cutover) describe the current
integration limits of the new actions.

`ACTION=restart` and `ACTION=stop` accept `ROLE=http`, `ROLE=worker`, or
`ROLE=scheduler` when that role is declared in the instance topology. Without
`ROLE`, both actions operate on all declared roles. An explicitly empty `ROLE=`
is an error. Confirmation names the selected role. A named action leaves the
other roles unchanged. Pending background recovery must first complete through
a plain restart. Restart waits for the selected role within the existing
90-second readiness budget; background roles do not require HTTP readiness.
Named actions record `degraded`, since they do not establish whole-instance
health; naming the only role a topology declares acts as the plain action. Reports
show inactive roles and raise `role_inactive` and `instance_degraded` as applicable;
a deliberately `stopped` instance raises neither. A plain restart or deploy starts and verifies
every declared role, including when deploying the same release again.
These starts also require admission through the
[persistent queue startup gate](#queue-startup-gate).

Unit enablement is the single source of truth for which roles should run.
A named `ACTION=stop` disables and stops its unit. Plain `ACTION=stop` disables
and stops every declared role. Stops therefore survive a reboot, and bootstrap
reruns preserve enablement. After upgrading the tools on a stopped instance,
run `ACTION=stop` again. The report raises `stopped_role_enabled` when an inactive
role on a stopped or degraded instance is still enabled and would start on reboot.
On a degraded instance, use `ACTION=stop ROLE=<role>` for that inactive role
to preserve the roles that are running.

Config changes, encryption key rotation, and rollback preserve enablement. They
apply their change and restart only enabled roles, including during failure
recovery. With no enabled roles, they start nothing and retain `stopped`. With
only some roles enabled, they check those roles and retain `degraded`. They only
record full health after every declared role is enabled and ready. Their internal
stops never disable units, so interruption cannot silently change reboot behavior.
A web-only instance does not prepare or validate a disabled worker's connection.

A named restart enables and verifies only its selected role. Plain restart and
deploy are the deliberate ways to start everything. Plain restart enables every
role; deploy enables every role after readiness succeeds. Failed deploy recovery
starts only the roles enabled before deployment, preserving disabled backgrounds
for later configuration changes or rollback. If final enablement or completion
fails after a deploy starts enabling previously disabled roles, the deploy stays
pending for reconciliation. It does not disable units or automatically restore
the old release.

Attempts record their selected role or enabled role set. Reconcile and failure
recovery honor that selection. Deploy reconciliation starts and enables all roles
only when the attempt reached the switch or health phase and its target release
is active. Otherwise it restores the recorded role set without enabling units.
A failed restart reconciliation records a terminal failure, allowing another
restart, stop, or deploy. Older attempts without the new fields retain their
previous behavior. Replaying a completed rollback or key rotation with the same
operation ID returns its recorded result without starting disabled roles.

The local tooling uses Python 3.11 or later, Git, Bash, OpenSSH, and Docker with
Buildx for release builds. The inherited builder resolves Go from the app's
`go.mod` and Node from `web/package.json`. It builds `cmd/artisan`, packages
`config/deploy.toml`, and includes public assets and private source maps when
present. Existing `client` and `web` packages use their declared build scripts.
The module's build tools are mounted read-only separately from the app source.
No compiler runs on the server.

The release builder pins Ubuntu, Go, and Node images by multi-architecture index
digest in `tools/deploy/docker/build-inputs.json`. To add a Go version or Node
major, add its reviewed digest there before building. Apt packages float within
noble: `ports.ubuntu.com` has no snapshot service, so arm64 silently ignores
`apt-get update --snapshot`. The manifest's `build_inputs` records the image
digests, Go, Node, npm, and C compiler versions, and a hash of the sorted package
inventory. Archive headers use the source commit's committer time, or zero for a
non-production build without a commit, and gzip headers omit the filename and
use time zero. Packaging the same stage with the same inputs produces identical
archive bytes. The release ID remains an input because the app embeds it.
Byte-identical compiler output across machines is not claimed.

SSH requires noninteractive authentication and an established host key. `make
bootstrap` of a profile not yet bound to a server, and recovery to a replacement
address, record an unknown host key on first contact (OpenSSH `accept-new`), which trusts whatever answers at that address. To verify
it instead, add the key to `known_hosts` first. A changed key is always refused, and
every other operation requires the recorded key. The private
key path saved for a host in `.deploy/settings.json` is enough: the tool offers only
that key and, on macOS, unlocks it from Keychain, so no SSH config entry is needed.
A key whose passphrase isn't in Keychain must be loaded into an SSH agent. Personal
aliases must resolve to the hostname and port in the committed profile. Host
mutations require root or passwordless `sudo -n`. Private configuration values
travel through stdin, never command-line arguments.

## Application input and identity

| File | Owner and contents |
| --- | --- |
| `deploy/project.json` | Tracked schema 1 project UUID and stable lowercase slug |
| `deploy/environments/<environment>.json` | Tracked schema 2 instance identity and public host profile |
| `deploy/topology.json` | Tracked schema 1 process roles |
| `config/deploy.toml` | Tracked nonsecret application settings |
| `.deploy/settings.json` | Ignored personal SSH settings |
| `.deploy/bootstrap/<instance UUID>.json` | Ignored pending host binding, mode 0600 |
| `.deploy/recovery/<instance UUID>.json` | Ignored recovery intent and replacement host binding, mode 0600 |
| `.deploy/releases/<instance UUID>.json` | Ignored public release-version cache |
| `dist/releases/<project UUID>/<instance UUID>/` | Completed local releases |

A project identity has this shape:

```json
{"schema": 1, "id": "10000000-0000-4000-8000-000000000001", "slug": "example"}
```

A schema 2 environment profile has this shape. Example UUIDs are placeholders for
new UUIDv4 identities, not identities to copy between applications.

```json
{
  "schema": 2,
  "environment": "production",
  "instance_id": "30000000-0000-4000-8000-000000000003",
  "host": {"address": "origin.example.com", "port": 22, "host_id": null},
  "runtime_environment": "production",
  "domain": "app.example.com",
  "certbot_email": "ops@example.com",
  "architecture": "amd64",
  "public_env": {},
  "edge_origin_lockdown": true,
  "retention": {"local_releases": 3, "server_releases": 5, "pre_migration_dumps": 7},
  "backup_policy": {"interval_hours": 24, "full_retention_days": 35, "pitr_days": 28}
}
```

Project, instance, and host UUIDs identify different owners. Renaming a checkout
does not change its project. A different product needs a new project UUID, and
each environment needs a different instance UUID. Slugs contain at most 20
characters. The combined `<project>-<environment>` name contains at most 31.

Bootstrap persists its pending marker before committing the host binding. A
retry may initialize a registry only when the saved binding and pending marker
match. A previously bound profile without that marker cannot initialize a new
empty host. The marker remains after interrupted preparation and disappears
only after the server confirms the expected host UUID.

Cloudflare is mandatory and implicit. Profiles have no `edge: none` option.
`edge_origin_lockdown` controls per-site origin restrictions. Domain values are
single lowercase ASCII DNS names. Profiles contain no credentials, disk names,
native-image selection, or application feature defaults. `public_env` accepts
only literal public `VITE_*` values.

`sync` shows a diff and never rewrites local identity or host registry state.
Changing a profile does not authorize adoption of unrelated server resources.

## Process topology

The application's `deploy/topology.json` declares supervision:

```json
{"schema": 1, "roles": [{"name": "http"}, {"name": "scheduler"}]}
```

`http` is required. `scheduler` and `worker` may each appear once. Every declared
role is required for readiness. No worker is implied by the project name or
configuration. Roles use framework-owned commands, not shell commands from JSON.

`tools/deploy/lib/topology.py` validates the declaration and records its canonical
SHA-256. `instance_units(entry)` is the single owner of the role-to-unit mapping:

| Role | Concrete systemd instance file |
| --- | --- |
| `http` | `foundry-http@<name>.service` |
| `scheduler` | `foundry-scheduler@<name>.service` |
| `worker` | `foundry-worker@<name>.service` |

The worker runs `queue:work --connection <app.worker.connection>`. Its connection
must be explicit in `config/deploy.toml` or the corresponding server environment
setting, including a supported `_FILE` alias. Restart resolves that selection
into the root-owned `worker.env` consumed by systemd. No product-specific queue
name is assumed.

## Host layout and registry

`tools/deploy/lib/contracts.py` owns resource derivation. The registry has schema
2 and layout version 2. Its root fields are `schema`, `layout_version`, `host_id`,
`revision`, `created_at`, `updated_at`, `tools`, `shared`, `projects`, and
`instances`. Instance keys are UUIDs. Each instance records its profile,
allocations, topology, framework generation, provisioning progress, and
configuration revision.

| Path | Purpose |
| --- | --- |
| `/etc/foundry/registry.json` | Root-owned public registry, mode 0644 |
| `/etc/foundry/host-secrets.json` | Root-only shared-service credentials, mode 0600 |
| `/etc/foundry/pki/` | Foundry CA and service TLS material |
| `/etc/foundry/cloudflare/zones/<zone-id>.ini` | Root-only DNS token, mode 0600 |
| `/etc/foundry/instances/<name>/` | Isolated private configuration, directory mode 0750 |
| `/var/lib/foundry/instances/<name>/state.json` | Public redacted release and operation state |
| `/var/lib/foundry/private/instances/<name>/` | Root-only journals and backup staging |
| `/var/lib/foundry/upgrades/<operation UUID>/` | Durable tool upgrade records |
| `/var/lib/foundry/reports/host-v2.json` | Public report snapshot |
| `/run/lock/foundry/host.lock` | Host mutation lock |
| `/run/lock/foundry/instances/<instance UUID>.lock` | Instance mutation lock |
| `/usr/local/lib/foundry/tools/<bundle digest>/` | Immutable installed tool bundle |
| `/usr/local/lib/foundry/current` | Active bundle pointer |
| `/usr/local/sbin/foundry-host` | Stable root mutation launcher |
| `/usr/local/bin/foundry-host-report` | Nonroot report entry point |
| `/opt/foundry/<name>/releases/<release>/` | Immutable application artifacts |
| `/opt/foundry/<name>/current` | Active application release pointer |
| `/var/lib/foundry/apps/<name>/` | Application-owned durable working directory |

Mutating operations take the host lock before the instance lock. A second
deployment, scheduled backup or certificate renewal waits up to 10 minutes for
the running one, then fails with `operation_locked` without changing anything.

The Linux account is `<project>-<environment>`. Database names use `f_` followed
by the instance UUID's 32 hexadecimal digits. Redis usernames use
`foundry-<instance UUID>`. The allocator selects HTTP ports from 8100 through
8999. Registry allocations bind these resources to UUID ownership markers.
Spelling alone never authorizes reuse.

Control paths reject symlink ancestors. Validated `current` pointers are explicit
exceptions. Atomic publication sets owner, group, and mode on the temporary file
and fsyncs it before replacement, then fsyncs the parent directory. An unchanged write must
match both content and metadata.

## Tool protocol and upgrades

`tools/deploy/protocol.json` owns protocol 2.0 and generation 5. The stable
launcher dispatches to the active immutable bundle. `hello` reports host tool
metadata without an application mutation. Requests include the client's protocol,
framework version, generation, and bundle digest.

A mutation from a newer client returns `host_upgrade_required`. A client below
the supported floor receives `client_too_old`. Generation 2 adds credential
replacement actions and continues to support generation-1 clients. Upgrade the host tools before using the new actions.
Compatibility adapters are deferred.

Generation 5 installs an `ExecStartPre` startup guard on every managed role and
adds coordinated queue cutover and Redis restore actions. Upgrade the host through
`ACTION=tools-plan`, then confirmed `ACTION=tools-apply` with that plan's token and
operation UUID, before planning a cutover. Ordinary deployment does not upgrade
host tools. The cutover checks both the owned unit files and the loaded units.
An absent guard, a drop-in, or a pending daemon reload blocks the operation.

The base client floor remains generation 1 so existing hosts can upgrade. Once
any instance has persisted queue compatibility state, host mutations require a
generation-5 client. Reads remain available. A pending cutover blocks tool upgrades.

Tool plans bind the candidate inventory digest to the current host state.
Application uses the reviewed plan token and durable operation UUID. Upgrades
move forward only. Interrupted application resumes the recorded operation rather
than creating a second upgrade or downgrading the host. Bundle staging excludes
Python bytecode and preserves the manifest's ownership of installed files.

## Release lifecycle and configuration

Activation validates the release identity and configuration before switching the
`current` pointer. It stops background roles before forward migrations, obtains
an off-host recovery point when migrations are pending, starts HTTP, waits for
readiness, and then starts the declared background roles. Readiness checks every
role and verifies its running executable through `/proc/<pid>/exe` against the
selected release. `ACTION=status` returns live observations per role;
`ACTION=logs ROLE=scheduler` reads that role's journal.

This ordinary activation sequence does not perform a queue-layout migration.
An existing instance whose Redis queues need coordinated activation receives
`queue_cutover_required`. Use the cutover actions below.

The nonroot report can read systemd state and HTTP readiness, but Linux may deny
access to another account's executable link. In that case it reports
`running_release_unverified` and an unknown overall instance status. A privileged
report must prove the executable identity; a matching unit configuration alone
does not establish which release is running.

Rollback requires an explicit retained `TARGET`, `EXPECTED_CURRENT`, and durable
operation UUID. The local CLI records the operation under `.deploy/operations/`
and prints its ID before sending the request. Repeating the same intent and UUID
returns its recorded result; changing the intent refuses. Rollback selects code
and does not reverse database migrations. `ACTION=reconcile` inspects interrupted
operations and requires process identity evidence before adopting a live release.
An unproven outcome remains blocked for reconciliation.
Queue cutovers and Redis restores use their own resume actions. Generic reconcile
does not bypass their startup gate or unresolved operation.

Configuration edits accept 1 through 16 string-valued settings in one revision.
The current application's `config:inspect` and `config:check` commands own key
validation, including supported `_FILE` aliases. The server snapshots referenced
files under the instance configuration directory before activation. It validates
candidate Redis key and channel inventories, widens grants before the switch,
and narrows them after every required role is ready. Failed narrowing remains a
durable cleanup obligation retried by reconciliation and later transitions.

A failed restart after configuration publication is `failed_applied`: the new
revision is on disk, and the operator can submit a corrective revision. The
operation's known outcome does not leave an unresolved lock. Public state records
configuration key names and redaction markers; private recovery metadata holds
secret-derived hashes. Generic editing cannot change allocated service identity
or encryption keys independently.

`ACTION="keys rotate"` replaces the active encryption ID and key atomically and
retains the prior active key and all older keys. A ring holds at most 32 keys.
Existing IDs cannot acquire different material. Rotation does not rewrite stored
application data or retire keys needed by old records or retained backups.
The CLI records and prints an operation UUID before sending the request. Retry
with `OPERATION=<uuid>` to reuse the server's privately persisted candidate and
original result, including after a lost response. An interrupted attempt must be
reconciled before replay; a failed attempt keeps its original failure result.
Keys and candidate hashes stay in root-only receipts, outside public status.

Every keyring change journals a key-archive prerequisite for its configuration
revision before selecting `app.env`. Reconcile, restart and corrective revisions
must complete `backup.archive_keys` before starting roles with those keys. Archive
failure leaves `key_archive.pending` visible in status. A completed archive records
its revision and completion time.

Success records the final Redis ACL target and pending narrowing together;
reconcile and the next lifecycle mutation finish outstanding narrowing. Terminal
configuration attempts also remove a leftover `app.env.next` only when its hash
matches their private intent. Before deployment stops background roles, it records
which were active. A proven failure before migration or seeding restores those
roles; failed restoration remains a visible `role_recovery_pending` obligation.
Uncertain migration or seeding outcomes still require explicit reconciliation.

Production builds require a clean committed tree and use `git archive` of HEAD.
Staging may use a dirty snapshot, recorded in release provenance. Build ownership
records prevent a second build from overwriting an orphaned container's output.
No automatic cleanup removes an unproven active Docker writer.

Both source snapshots preserve relative symlinks whose targets and link chains
stay inside the snapshot root, including `system/.agents/skills -> ../.claude/skills`.
Copying preserves each link without copying its target. Absolute or escaping
symlinks, cycles, entries beneath symlinked directories, hardlinks, and special
files are refused.

<a id="recipe-queue-cutover"></a>
## Coordinated Redis queue cutover

Use this operation to move one instance's Redis job queues to layout 3. Every
managed role stops, including HTTP. About one minute of full downtime is a target,
not a deadline. The owner chooses the window by running apply. Once a queue
migration may have run, recovery moves forward. The tool never automatically
starts the old release after that boundary.

These generation-5 tools and the release-owned `deploy:jobs-inspect` command ship
in v0.15.0. An absent command blocks planning. Restore resume reconciles an
unstarted intent only after exit proof and the module's safety and drift checks.
The [local integration checks](../../tools/deploy/README.md) exercise the consumer
command and restore intake; they do not certify a production host's recovery setup.

Before planning, install generation-5 host tools, configure the existing backup
repository and age recipients, and retain the corresponding recovery identity.
The owner must attest that only the instance's managed topology roles use its job
namespaces. External publishers, workers, relays, or operator processes invalidate
that attestation. The tool supports only its Foundry-owned local TLS Redis
allocation and verifies exclusive ownership of the instance's captured scopes.

### Plan and choose the window

Build or select the candidate without stopping clients:

```sh
ENVIRONMENT=production ACTION=cutover-plan RELEASE=build make deploy
```

The tool validates the archive, configuration, native runtime, and configured
storage bindings. For a worker role, it also validates the candidate's explicit
`app.worker.connection`, including environment and `_FILE` overrides. A framework
default job connection does not replace this worker selection. Plan and apply
perform this validation before downtime. The tool retains the candidate and may widen owned Redis grants.
Planning neither stops roles nor migrates queues. The candidate's
`artisan deploy:jobs-inspect --format json` supplies every configured Redis job
connection and queue through the release's normal `--config` and `--env-file`
settings. Its schema-1 contract reports `supported_layout: 3`,
`coverage: "declared"`, connections, queues, observed layouts, eligibility, and
safe blocker codes. Declaration coverage alone is not proof of storage coverage:
the deploy tool completes a privileged SCAN and rejects any key outside the
release's allowed key inventory. There is no manual queue list. Unknown providers,
layouts, policies, orphan keys, or incomplete discovery block the plan.
Recognized blockers return `applicable: false`, logical queue inventory, and safe
`blockers` with connection and queue names. Such a result has `plan_token: null`;
confirmation cannot make it applicable. Malformed inspection still fails outright.

The public plan contains these fields:

| Fields | Meaning |
| --- | --- |
| `schema`, `plan_token`, `checked_at` | Schema 1, immutable plan digest, and preflight time |
| `applicable`, `blockers` | True and an empty list for an applicable plan |
| `instance`, `instance_id`, `host_id` | Bound deployment identity |
| `source`, `target`, `candidate_sha256` | Old and candidate release versions and candidate artisan checksum |
| `roles`, `original_enabled_roles`, `original_running_roles` | All roles to start on success, original enablement, and the running set to restore before migration |
| `queues` | Logical `connection`, `queue`, observed `layout`, and `target_layout: 3` for each queue |
| `downtime`, `warning`, `owner_attestation` | Full downtime, rollback restriction, and the owner's no-external-clients attestation |
| `snapshot` | Capture after verified drain, with encrypted off-host verification |
| `database_migrations_pending`, `seeders` | Database work observed during preflight and the required seeding step |
| `queue_cutover_required` | False for a complete no-Redis inventory |
| `forward_recovery` | Null for a normal plan; prior operation identity, progress digest, database and seed status, and warning for a replacement plan |

Private intent additionally binds configuration and referenced-file fingerprints,
topology, inventory, the startup gate, and the exact installed tool bundle. Changes
to these inputs invalidate the plan. A no-Redis operation skips queue migration and
Redis capture; the remaining activation sequence still runs. Layout-3 queues are
eligible for repeated migration. The tool deduplicates matching physical aliases.

After reviewing the plan, supply its token and a new UUIDv4. Apply prints the saved
plan again before confirmation. For an approved automation window:

```sh
ENVIRONMENT=production ACTION=cutover-apply \
	PLAN_TOKEN=<reviewed-token> OPERATION=<new-uuid-v4> CONFIRM=yes make deploy
```

Without `CONFIRM=yes`, type `yes` at the confirmation prompt. Apply and resume
reject `ROLE` and `RELEASE` overrides. They use all roles and the retained candidate
bound by the token. Preserve both identifiers for recovery.

### Activation and resumption

Apply repeats preflight before stopping anything, persists the accepted operation,
and closes startup permission. A systemd executor runs the pinned tool bundle
independently of the SSH connection. Its sequence is:

1. Stop HTTP, scheduler, and worker roles using their existing shutdown semantics.
   Verify unit jobs, process IDs, role cgroups, instance-owned processes, and
   previous executor children. A timeout is not exit proof. Forced termination
   records `drain_failed` and requires review before any snapshot or migration.
   Before stopping, persist the host boot ID and each role's invocation ID. Persist
   each role's clean drain as it completes. A reboot with unresolved drain or a
   changed invocation before completion requires review, even if units now appear inactive.
2. Repeat inventory and storage coverage checks. Capture the in-window Redis
   snapshot and persist verified evidence before dispatching a queue migration.
3. Use the candidate's `jobs migrate-layout --connection C --queue Q --confirm
   --format json` for each physical queue. Reinspect to confirm layout 3.
4. Inspect database migration status. If migrations are pending, take the existing
   database backup, run forward migrations, and confirm no migrations remain.
5. Run seeders, select the candidate, prepare its worker runtime environment while
   startup is still blocked, admit its exact binary checksum, start the
   roles, verify health and executable identity, and enable all declared roles.

Selection and health are separate facts. `state.current` records the verified
artifact observed at the `current` pointer even when pointer publication is
uncertain or health fails. A selected candidate remains blocked after failure;
its presence in `state.current` is not startup admission. Restore and subsequent
forward planning use that observed identity without starting the fenced release.
Restore planning and acceptance also reconcile the selected artifact after hard
executor death, when no exception handler could publish state. That identity is
retained through restore completion and the next cutover plan.
Only an unresolved operation constrains selection to its source or target.
Completed cutovers remain history: after a later ordinary deployment, restore
planning uses the currently selected release.

Apply returning `outcome: running` confirms submission, not healthy completion.
Read `ACTION=status make deploy`. `state.cutover` records the operation ID, phase,
outcome, per-queue progress, database and seed progress, snapshot evidence, and
safe errors. Status also reports executor state and a suggested next action.
Reports expose cutover progress and queue compatibility state.

Before migration dispatch, a proven safe failure restores the source and its
originally running roles, preserving enablement separately. A disabled-but-running
worker runs again without being enabled; an enabled-but-inactive role stays inactive.
An unproven drain, changed inputs, or failed recovery
keeps startup blocked. Before each migration dispatch, the tool persists the
layout-3 requirement and prohibits old-release recovery. A lost response therefore
cannot cause an old-code restart. This restriction also applies when inspection
already observes layout 3.

After interruption, inspect status and explicitly resume the same intent:

```sh
ENVIRONMENT=production ACTION=cutover-resume \
	PLAN_TOKEN=<same-token> OPERATION=<same-uuid-v4> CONFIRM=yes make deploy
```

A live executor is joined, not duplicated. After reboot there is no automatic
executor resume. During candidate readiness, admission is bound to the attempt's
recorded boot and live executor invocation. Enabled roles cannot restart the
unresolved candidate on another boot, with missing or invalid boot evidence, or
under another executor invocation. The guard refuses startup and state reads mark
the attempt review-required, even before an operator runs a recovery action.
Use confirmed `stop`, audited `cutover-drain-resolve`, then explicit `cutover-resume`.
Resolution leaves the gate closed; resume records a new attempt binding before
starting roles. Only a startup attempt recorded as successfully healthy retains
ordinary admission across reboot, subject to the existing compatibility gate.

Confirmed queues are not migrated again; an uncertain queue call can repeat
idempotently. Snapshot retries retain the original captured artifact. An interrupted
initial capture without a complete archive requires review. Database uncertainty
can be reconciled when status proves no pending migrations. An uncertain seeder
is never repeated automatically. Forced termination, unresolved database or seed
outcomes, and post-migration failures require operator review and a lead-prepared,
owner-approved forward recovery. Reusing a completed operation returns its receipt.

### Apply a reviewed forward fix

For an unresolved operation with a layout-3 floor and a defective candidate, the
lead prepares a replacement release and the owner approves its recovery behavior.
If necessary, use confirmed `ACTION=stop` to contain the existing executor first.
Forced termination and unresolved boot/drain evidence remain review blockers.
Use the explicit drain resolution below after review; forward recovery does not
bypass process-exit proof.

```sh
ENVIRONMENT=production ACTION=cutover-forward-plan RELEASE=<replacement-version> make deploy
```

The plan binds the replacement artifact, the prior operation's exact progress,
configuration, topology, and the same validated queue inventory. It retains the
layout floor and prior snapshot bindings. Review the prior database and seed
outcomes: applying a replacement explicitly authorizes that replacement's pending
migrations and seeders. They must account for side effects the prior artifact may
already have completed. Ordinary resume never makes this decision automatically.

```sh
ENVIRONMENT=production ACTION=cutover-forward-apply \
	PLAN_TOKEN=<forward-plan-token> OPERATION=<new-uuid-v4> CONFIRM=yes make deploy
```

Normal `cutover-apply` cannot accept a forward-recovery plan. The explicit action
archives the superseded progress and receipt, keeps startup blocked, and starts
a new supervised operation for the replacement. Original snapshot evidence stays
available for reviewed restore. If interrupted, use `cutover-resume` with the
replacement plan's token and operation UUID. After a completed Redis restore,
use ordinary `cutover-plan` for the new forward activation instead.

### Resolve an uncertain drain

Only after a completed drain and reboot, before any candidate startup attempt,
can a stopped unit's zero-main-process evidence exempt a guard refusal from drain
review. This includes both `Result=exit-code` and `Result=start-limit-hit`.
Empty cgroups and absence of instance-owned clients must still be proved.

Before candidate startup, the tool persists an attempt UUID, start time, boot ID
and supervised executor invocation ID. While the attempt is unresolved, each
admission check requires that same boot and live executor. The startup guard reads
this evidence without taking coordinator locks or overwriting shared state;
containment and recovery persist the resulting review requirement.
Any failed startup or readiness check, unsuccessful unit exit, kill, timeout, or
interruption requires confirmed, audited drain resolution before resume or forward
recovery. A later guard-refused invocation cannot establish that the earlier
attempt did no work. The failed or uncertain attempt remains in `candidate_start`,
and `drain_failed` directs the operator to `cutover-drain-resolve`. Forced termination
and unresolved boot evidence also retain that requirement.

For an unresolved cutover, missing startup history from migration dispatch onward
(or any later state where candidate startup is possible) is unknown, not evidence
that no attempt ran. Malformed startup history is also unknown. The tool retains
a fingerprint of that evidence in `candidate_start` with outcome `history_unknown`,
blocks startup and requires audited resolution before resume or forward recovery.
A clean stop does not remove this requirement. A later loss of history requires a
new review; an earlier approval cannot authorize it.

Contain the executor and roles with confirmed `ACTION=stop`. The lead reviews
interrupted work and external side effects, and the owner approves the resolution.
Containment records `drain_failed` for an interrupted or unknown candidate attempt
and returns `cutover-drain-resolve` as the next action. No failed resume is needed
to make resolution available. Process-exit and client checks still apply.
Record that decision with the active operation UUID:

```sh
ENVIRONMENT=production ACTION=cutover-drain-resolve OPERATION=<active-uuid-v4> \
	REVIEWED_BY=<reviewer-identity> REASON='<reviewed evidence and resolution>' \
	CONFIRM=yes make deploy
```

This action requires executor exit, stopped roles, empty cgroups, no remaining
clients, installed guards and closed startup permission. It records who, why,
when, the boot identity, each role's observed invocation/exit evidence, and the
failed or uncertain candidate attempt being approved. It
clears the review flag without claiming a clean drain or starting any process.
The approval applies only to that attempt and those observations; a new attempt, changed invocation or boot
requires fresh proof. Do not put secrets in the reason. Then resume the same
intent, plan a reviewed forward fix for a broken candidate, or plan Redis restore.

### Replace a pre-dispatch plan

An admitted request can publish an undeclared queue between preflight and drain
completion. Post-drain coverage then blocks the operation and keeps startup
fenced. Preserve the queue data. The lead prepares a candidate that declares the
missing queue, and the owner approves supersession:

```sh
ENVIRONMENT=production ACTION=cutover-abort-plan RELEASE=<corrected-version> \
	REVIEWED_BY=<reviewer-identity> REASON='<why the inventory must change>' make deploy
ENVIRONMENT=production ACTION=cutover-abort-apply PLAN_TOKEN=<reviewed-token> \
	OPERATION=<new-uuid-v4> CONFIRM=yes make deploy
```

Planning requires durable proof that neither snapshot capture nor queue/database
migration or seeding has begun, and that no layout-3 floor exists. It validates
the corrected candidate's configuration, inventory and complete stored-key coverage.
The token binds the reviewer, reason, replacement artifact and prior progress.
Apply rechecks those conditions, archives the superseded operation, and starts a
new cutover without opening the fence or deleting queue data. Resume the replacement
with `cutover-resume`. Once dispatch has begun, use the forward or restore procedures.

### Queue startup gate

`state.queue_compatibility` persists separately from bounded attempt history. It
contains `schema`, the required layout `floor`, startup `permission`, and an
`admitted` map of release versions to artisan checksums. The layout-3 floor cannot
be removed or lowered through state writes. Every role's startup guard checks
the selected release against this record, including systemd restarts and reboot.
Missing gate metadata alongside cutover progress fails closed. Dispatch progress
and rollback prohibition must agree with the persisted layout-3 floor. A proven
no-Redis cutover with no earlier floor retains ordinary retained-release rollback.

`ACTION=restart` refuses a pending cutover, closed permission, or an unadmitted
release. `ACTION=rollback` refuses an old unadmitted target before side effects;
retaining its files does not make it compatible. Ordinary activation cannot clear
a closed gate. Successful forward activation admits verified compatible code.

Confirmed `ACTION=stop` remains available for containment. During a pending cutover
it first stops and proves exit of the executor, closes startup permission, and
records a hold. It then stops and disables the selected roles. It never runs old
artisan code or restarts incompatible processes. Use the matching resume action
after review. Stopping an already admitted compatible release preserves its
admission for a later ordinary restart.
That terminal stop records `stopped` or `degraded` from the remaining running
roles and returns `next_action: restart`. Actual pending-operation containment
keeps its progress and hold, with an honest unknown lifecycle status.

### Redis snapshot evidence

The snapshot captures only the instance's exact allowed job keys after verified
drain. It uses SCAN, then atomic DUMP and PEXPIRETIME reads, preserving absolute
expiration. It never captures a shared database as one instance's backup. Ownership
checks include the Foundry Redis journal, persisted and live ACLs, storage identity,
and other instances' grants. Unknown keys or shared scopes block capture.

The existing backup pipeline encrypts with the configured age recipients and
uploads to the existing backup bucket at
`redis/<instance UUID>/<snapshot UUID>.json.age`. Downloaded ciphertext must match
before capture is complete. Public `state.cutover.snapshot` evidence contains:

- `schema`, `status`, `snapshot_id`, and `operation_id`;
- `host_id`, `project_id`, `instance_id`, `source_release`, and `configuration_revision`;
- `inventory_sha256` and `scope_sha256`;
- `captured_at`, `completed_at`, `key_count`, and `encrypted_bytes`;
- `sha256` of encrypted bytes and `object_key`.

Freshness means capture inside this operation's downtime window. Evidence contains
no credentials, private key contents, or job payloads. Root-only journals retain
the capture intent, ciphertext, and receipt for reconciliation. See the
[Redis snapshot module reference](../../tools/deploy/redis-snapshot.md) for bounds
and record ownership. These are operation snapshots, not scheduled Redis backups.

### Restore a Redis snapshot

Restore is explicit, confirmed, and never automatic. It can replay jobs whose
external side effects already completed. Review application idempotency and database
changes before restoring. The operation retains the startup gate even on success;
it does not authorize old code or reverse database migrations.

Restore scope comes from the immutable validated inventory saved with the selected
snapshot's evidence, not a new migration-eligibility check of damaged queue values.
Ownership and exact-key coverage still apply. A missing binding or different
snapshot evidence blocks restore planning.

1. Read status and retain the selected snapshot's identity and evidence. If an
   executor is still active, use confirmed `ACTION=stop` to contain it first.
   Forced worker termination remains a review blocker.
2. Supply the private age identity through a local file or the hidden prompt.
   Choose a new restore UUID, distinct from the original cutover UUID:

   ```sh
   ENVIRONMENT=production ACTION=redis-restore-plan \
       SNAPSHOT=<snapshot-uuid> OPERATION=<new-restore-uuid-v4> \
       FOUNDRY_SECRET_AGE_IDENTITY_FILE=/private/path/recovery.agekey make deploy
   ```

   The identity travels through the private stdin envelope, never argv or public
   receipts. The server writes it at `<operations dir>/<operation UUID>/restore.identity`
   inside a root-owned mode-0700 operation directory, with file mode 0600, then
   removes the identity after planning. Planning downloads and verifies the ciphertext, decrypts privately,
   validates serialized values offline, and binds the exact restore and deletion
   inventory. Retain the returned `plan_token` and `operation_id`. Review `snapshot`,
   `scope_sha256`, `restore_keys`, `remove_keys`, and the warning
   `restore_may_replay_external_side_effects`.
3. Confirm the saved plan in the chosen window:

   ```sh
   ENVIRONMENT=production ACTION=redis-restore-apply \
       PLAN_TOKEN=<restore-token> OPERATION=<restore-uuid-v4> CONFIRM=yes make deploy
   ```

   The executor stops and verifies every role, captures a fresh safety snapshot,
   then restores exact keys and deletes only planned extra keys. It never flushes
   Redis. Expired keys are skipped; natural expiration does not extend lifetimes.
   Changed scope or an unstarted plan's changed key inventory blocks application.
4. Inspect status. After interruption, use `ACTION=redis-restore-resume` with the
   same token, UUID, and confirmation. The original safety snapshot is retained.
   Once M4 has recorded dispatch, it always delegates to M3 `resume_restore`,
   including a crash before M3 publishes its first progress record. M3 owns
   same-intent reconciliation and all snapshot and drift checks; M4 bypasses none.
   Completion records `state.cutover.restore`, including the restored and safety
   snapshot IDs, scope checksum, completion time, and restored and removed counts.
5. Keep roles stopped. Review a new `cutover-plan` for forward recovery before
   starting compatible code, including when the restored queues are layout 2.

<a id="recipe-storage-probe"></a>
## Storage probe

For an instance with cloud disks, deployment runs the release's
`artisan deploy:storage-probe` after `config:check` and before switching releases.
Storage or credential configuration edits run it against the candidate settings
before publication. Unchanged settings and unrelated edits do not run it.
Rollback, restart, recovery (rehearsal and takeover), and capacity changes do not
run the probe.

The command writes, reads, compares, and deletes a random 32-byte object on every
configured disk, one disk at a time. Each bucket briefly contains an object named
`foundry-deploy-probe/<32 random hex characters>` within the disk's namespace.
Write, read, and delete share a 20-second budget per disk. Failed reads and writes
with applied or unknown outcomes get a separate 10-second cleanup attempt.
The probe boots only storage infrastructure, without opening database or Redis
connections.

A failed probe refuses the deployment or configuration edit. Its safe report names
the disk, stage, and storage error code. Check that disk's bucket and credentials.
If deletion fails, `leftover_key` identifies the probe object to remove. No list
permission is needed to prove deletion. A release without this command is refused
with `storage_probe_unavailable` when the instance has cloud disks. Register the
command in the application's console catalog and rebuild the release:

The command catalog and settings loader remain application-owned. Compose the
builder with web serving disabled, then call the public helper:

<!-- compiled-source: tests/fixtures/consumer/tooling/readiness.go#storage-probe -->
```go
func ProbeStorage(ctx context.Context, builder *application.Builder, output io.Writer) error {
	return application.ProbeStorage(ctx, builder, output)
}
```

The helper writes one JSON object, including failures, and returns a non-usage
error if any disk fails. Keep stdout for this report. Exit status 2 tells the
deploy tool that the command is unavailable.

## Bootstrap prompts and secrets

The CLI creates project and instance UUIDs when their files do not exist. Its
public prompts collect the project slug, environment, canonical SSH hostname,
port, runtime environment, public domain, and certificate contact email. Host
inspection discovers the architecture. `deploy/topology.json` must already declare
the application's roles. Bootstrap presents package, allocation, and site findings
before applying the digest-bound plan.

Secret prompts do not echo input. Automation supplies private file paths through
these environment variables:

| Variable | File contents |
| --- | --- |
| `FOUNDRY_SECRET_CLOUDFLARE_DNS_TOKEN_FILE` | Zone-scoped Cloudflare token |
| `FOUNDRY_SECRET_BACKUP_S3_FILE` | JSON object with `endpoint`, `region`, `bucket`, `access_key_id`, and `secret_access_key` |
| `FOUNDRY_SECRET_AGE_RECIPIENTS_FILE` | Nonempty JSON array of public age recipients |
| `FOUNDRY_SECRET_APP_STORAGE_FILE` | JSON object with `credentials` and `disks` tables |
| `FOUNDRY_SECRET_ENCRYPTION_FILE` | Optional JSON object with `key_id` and `key` |
| `FOUNDRY_SECRET_CONFIG_FILE` | JSON object of 1 through 16 configuration keys and string values for `ACTION=config`, or the initial application configuration at bootstrap |

The client sends these values in the JSON request on SSH stdin under `secrets`.
Values never belong in command arguments, tracked profiles, or release archives.
The server records bootstrap intake as root-only mode 0600
`/etc/foundry/instances/<name>/bootstrap-secrets.json`, bound to the instance UUID.
App-readable runtime values are separate from root-only control credentials.
Rerun `make bootstrap` with corrected private inputs while their consuming step
is unfinished: `app_storage` and `configuration` until `configuration`,
`backup_s3` and `age_recipients` until `backup`, and `cloudflare_dns_token` until
`certificate`. Only supplied inputs change. Encryption cannot be replaced through
bootstrap. Once a step completes, use `ACTION=config` or the corresponding
credential replacement command below; missing initialized secrets require recovery.

A backup correction preserves the repository cipher passphrase and host/cluster
identity. It is refused if any instance completed `backup`, local history records
backup use or an unresolved attempt, or repository inspection cannot prove an empty
repository. Corrected credentials are inspected through a private temporary
pgBackRest configuration before replacing the selected settings. A destination
change must also prove the old destination empty. DNS correction preserves the
recorded zone and is refused if any instance in that zone completed `certificate`.
These guards prevent a failed new instance from overwriting shared working secrets.

Bootstrap also accepts initial application configuration that the first deploy
needs before any release exists, such as default-admin seed credentials
(`ADMINS__SEED__SUPERADMIN_PASSWORD`). The same key rules as `ACTION=config`
apply. Encryption and storage keys use their own inputs. Interactive bootstrap
asks for it once; leave it blank when the application needs none. Later changes
use `ACTION=config`.

When encryption input is absent, the server generates the key ID and 32-byte key
together. Key material uses canonical unpadded base64url. The initial previous-key
table is empty. Recovery identities for age remain in the operator's password
manager or offline custody, never on the production host.

## Credential replacement

All replacement actions require confirmation (`CONFIRM=yes` for reviewed
automation). Use the private input files above; never pass a credential value in
an argument. The host lock and affected instance locks serialize replacements
with deployments and bootstrap. Each affected root-only bootstrap record is
atomically updated, so a later bootstrap with no secrets keeps the replacement.
Do not leave old secret input files selected when rerunning bootstrap: explicit
stale values for completed steps are still refused.

| Action | Private input | Scope and verification |
| --- | --- | --- |
| `backup-keys-rotate` | `FOUNDRY_SECRET_BACKUP_S3_FILE` | Shared backup repository; `pgBackRest check` verifies the new keys |
| `backup-recipients-rotate` | `FOUNDRY_SECRET_AGE_RECIPIENTS_FILE` | Selected instance; immediately uploads and download-checks a fresh encrypted key archive |
| `dns-token-rotate` | `FOUNDRY_SECRET_CLOUDFLARE_DNS_TOKEN_FILE` | Every instance using the recorded Cloudflare zone; zone lookup and renewal dry-runs verify the token |

For example, after preparing a private replacement backup JSON file:

```sh
FOUNDRY_SECRET_BACKUP_S3_FILE=/private/path/backup.json \
  make deploy ENVIRONMENT=staging ACTION=backup-keys-rotate
```

Backup key replacement accepts only the existing endpoint, region and bucket.
It preserves the repository cipher passphrase and host/cluster identity. A failed
check restores the previous settings and pgBackRest configuration. Success writes
a fresh recovery archive for every configured instance because those archives
contain repository credentials. If archive refresh fails after replacement, the
command reports that the keys were replaced and archives remain incomplete;
inspect `ACTION=status` before explicitly retrying the same values.

Recipient replacement keeps old archives untouched. Download a fresh archive and
**prove decryption with the new identity before discarding the old identity**.
Keep identities needed for retained old archives. An archive upload failure leaves
the new recipients selected and reports incomplete archive refresh; inspect status
before retrying. Decryption identities remain off the production host.

DNS token replacement requires a token for the instance's recorded zone. The zone
`.ini` is shared by its instances and is replaced atomically. Renewal dry-runs use
`--no-random-sleep-on-renew`; failure restores the previous token.

Replace application R2 keys with `ACTION=config`, using
`FOUNDRY_SECRET_CONFIG_FILE` to supply `APP__SERVICES__CREDENTIALS` as a JSON string
(or its supported `_FILE` alias). The initial `app_storage` and `configuration`
bootstrap values can correct `app.env` until its `configuration` step completes.
Later applies preserve the selected file; a missing file after `configuration`
completes requires recovery instead of recreation from old bootstrap values.

The CLI reuses one SSH master per invocation, with a 120-second persistence window
and a private per-user control directory under `/tmp`. It closes the master when
the invocation finishes. Connection establishment gets three attempts with a
30-second timeout; keepalives tolerate eight missed replies. Commands are never
automatically replayed. SSH exit 255 means the connection failed or dropped, and
a mutating command's outcome is unknown. Run `ACTION=status` before retrying;
follow the existing reconciliation procedure for an unresolved operation.

## PostgreSQL, Redis, and foreign services

Foundry uses the distro PostgreSQL major for the supported Ubuntu release:
16 on Ubuntu 24.04 and 18 on Ubuntu 26.04. Its cluster is `<major>/foundry`, with
`postgresql@<major>-foundry.service`, its own configuration and data directories,
and an explicit Unix socket for peer administration. TCP allocation starts at
55432 and can use the next free port through 55531. Runtime configuration uses
the recorded allocation. App connections require verified TLS and exact
instance database and role grants.

The cluster runs as the dedicated system user and group `foundry-postgres`,
with `nologin` and home `/var/lib/foundry/postgresql`. Data lives beneath that
home at `<major>/foundry`; configuration lives at `/etc/postgresql/<major>/foundry`.
`pg_createcluster --user foundry-postgres` initializes the matching database
superuser. Peer administration uses that account on `/run/foundry/postgresql`
at the allocated port. The distro `postgres` account and foreign clusters are
not used. `postgres.cluster_owner()` exposes the service identity;
`postgres.cluster_settings(host, registry)` includes its `archive_command`,
which PostgreSQL executes directly as `foundry-postgres`.

An interrupted first creation can retry under its private install journal.
Only a journaled creation that has never attempted a start, has no successful
start record, and contains only recognized initdb artifacts may be dropped
with `pg_dropcluster <major> foundry` and recreated at the same allocation.
Start artifacts, user database/relation files, unknown paths, active services,
nonempty server logs, and uncertain ownership require operator reconciliation.
Established clusters are never reset by this recovery path.

Redis uses `redis-server@foundry.service`, `/etc/foundry/redis/`, and
`/var/lib/foundry/redis/`. Its TLS port is 56379 and its plaintext listener is
disabled. The default ACL user is disabled. Each instance receives only literal
key and channel prefixes reported by its application's `redis:keys` command,
including validated dot-delimited child namespaces. ACL changes preserve other
users' bytes and retain private transition journals. Redis uses AOF with
`appendfsync everysec`, RDB snapshots, and `noeviction`; host failure can lose
roughly the last second of acknowledged Redis writes.

Installed compatible binaries are reused. Missing packages require an apt
simulation containing only new installations at approved versions. An upgrade,
removal, reconfiguration of an existing package, or unrelated broken package
blocks provisioning. Installation uses `NEEDRESTART_SUSPEND=1` and a journaled
`policy-rc.d` that denies service starts. An existing unknown policy causes a
refusal. Recovery is limited to the owned transaction.

Every accepted nginx binary, including one installed outside distro packages,
must expose its effective configuration through `nginx -T`. Uninspectable
configuration and conflicting domain declarations block provisioning.

Foreign PostgreSQL clusters and Redis instances are never adopted, authenticated
to, reconfigured, or restarted. Package-created unused defaults may be disabled
only when the installation journal proves they were absent beforehand; their
data remains intact. Shared nginx changes affect only the owned site and use
configuration validation followed by a graceful reload. Foreign configuration,
PostgreSQL and Redis process identities, data, and reachability must remain
unchanged. Nginx retains its master PID while a reload may replace workers.
## Service capacity

New PostgreSQL and Redis allocations use the smaller of the server's `MemTotal`
and its numeric cgroup v2 `memory.max` limit, in MiB (`M`). At least 1536 MiB is
required. Recovery sizes for the replacement host. Allocations are recorded;
resuming provisioning, deploying, or upgrading tools never resizes them.
Existing v0.5.x allocations keep their recorded values, including the old
PostgreSQL defaults, until an operator explicitly runs:

```sh
ENVIRONMENT=production ACTION=capacity make deploy
```

The command prints current and proposed values and the units that will restart,
then asks for confirmation. PostgreSQL and Redis restart through their owned
units, causing a brief application interruption. Apply verifies effective
settings and readiness for instances recorded as `healthy` or `rolled_back` before
acknowledging the new allocations. Never-deployed and stopped instances stay as they are.
Shrinking is supported. When values already match, apply does nothing. Rehearsals,
pending recovery and interrupted deployments cannot change capacity.

Sizing uses these rules (memory amounts below are in MiB):

- PostgreSQL `shared_buffers`: `M / 4`, rounded down to 64, capped at 8192.
  `effective_cache_size`: `M / 2`, rounded down to 64.
- `max_connections`: `floor(M / 40)`, clamped to 40–200.
  `work_mem`: `floor(M / 8 / max_connections)`, clamped to 4–16.
- `maintenance_work_mem`: `M / 16`, rounded down to 16, clamped to 64–1024.
  Recorded PostgreSQL budget: shared buffers + maintenance memory + connections × work memory.
- Redis `maxmemory`: `M / 8`, rounded down to 64, clamped to 128–4096.
  Its recorded budget is twice `maxmemory`; `maxclients` remains 256.

| RAM (MiB) | shared_buffers | effective_cache_size | Connections | work_mem | maintenance_work_mem | Redis (MiB) |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1536 | 384MB | 768MB | 40 | 4MB | 96MB | 192 |
| 1950 | 448MB | 960MB | 48 | 5MB | 112MB | 192 |
| 3900 | 960MB | 1920MB | 97 | 5MB | 240MB | 448 |
| 7900 | 1920MB | 3904MB | 197 | 5MB | 480MB | 960 |
| 32000 | 8000MB | 16000MB | 200 | 16MB | 1GB | 3968 |
| 64000 | 8GB | 32000MB | 200 | 16MB | 1GB | 4096 |

These are allocation defaults, not workload measurements or hard process memory
limits. After an interruption, rerun `ACTION=capacity` to resume the approved
sizes; changing RAM does not replace an unfinished plan. If a systemd job is still
pending, wait and rerun the command. The tool retries a restart when needed and
verifies the new invocation. If restarting keeps failing, recreate the server with
`ACTION=recover`. A new resize refuses while a scheduled backup is running; wait
for it to finish and rerun. Other mutations remain blocked while a capacity
transition is pending.

## Cloudflare and certificates

Each site replaces forwarding headers and trusts `CF-Connecting-IP` only from
the versioned Cloudflare ranges. Optional origin lockdown is site-local, with
loopback available for host probes. Ranges older than 90 days produce a warning;
older than 180 days block site planning while existing configuration remains
renderable for recovery. No server-side network fetch updates the range file.

The token needs Zone Read and DNS Edit for exactly one active zone containing
the profile domain. Bootstrap resolves that zone and stores its token at
`/etc/foundry/cloudflare/zones/<zone-id>.ini`, root-owned mode 0600. Existing
credentials are immutable during ordinary bootstrap; rotation requires an
explicit operator procedure.

Certbot uses DNS-01 with `python3-certbot-dns-cloudflare`. The owned lineage is
named for the domain. Reuse checks instance ownership, hostname, expiry, renewal
configuration, token path, timer state, and the reload hook. Foundry uses
`--config-dir /etc/foundry/letsencrypt`,
`--work-dir /var/lib/foundry/letsencrypt`,
`--logs-dir /var/log/foundry/letsencrypt`, and `--no-directory-hooks` for issuance,
renewal, and the renewal dry-run. The `foundry-certbot-renew.timer` and service
renew these isolated lineages. An explicit private `cli.ini` excludes inherited
Certbot options; owned renewal files may not contain pre/post hooks. The deploy
hook runs `nginx -t` before gracefully reloading nginx. Foundry never reads or
changes `/etc/letsencrypt` or manages the distro `certbot.timer`. A03 readiness
checks apply only inside Foundry's certificate namespace. Provisioning requires
a renewal dry-run with deploy hooks. After valid production issuance, a failed
dry-run is deferred only when a separate probe of the Let's Encrypt staging
directory fails to connect, times out, or returns HTTP 5xx. Other dry-run failures
remain fatal. A deferred check prints a warning and journals `renewal_deferred`
on the instance certificate entry. The renewal timer continues unchanged.

While deferred, the host report raises `certificate_renewal_unverified` and sets
`complete` to false. The certificate output includes the last check time and safe
failure detail. The next deploy retries, including an unchanged-release deploy.
A successful retry clears the marker. A failed retry preserves the problem and
updates its detail without failing deployment. `dns-token-rotate` still requires
successful dry-runs for every affected instance.

Container acceptance uses a guarded synthetic CA path described below. It does
not call Cloudflare or Let's Encrypt and cannot establish real DNS-01 provider
acceptance.

## R2 attachments

Applications declare private disks in their configuration. Bootstrap's
`app_storage` envelope supplies the full named `credentials` and `disks` tables
as server-side settings. Each disk uses `driver: "r2"`,
`config: {"visibility": 0}`, and a `cloud` object containing `bucket`, `region`,
`endpoint`, `namespace`, and the named `credentials` reference. The credential
source has `mode: "static"`, `access_key`, and `secret_key`. R2 uses region `auto`
and its explicit HTTPS account endpoint. The buckets must already exist.

The registry records each runtime disk's bucket and namespace prefix.
Overlapping prefixes across instances are refused, even when endpoint spellings
differ. Within an instance, overlapping aliases must have identical bindings.
That check covers one host's registry, so give instances on different hosts
separate buckets yourself. Use a distinct bucket for the backup repository, and
one backup bucket per host: the repository path is fixed, so two hosts cannot
share one.

Credentials limited to object access on their bucket are enough; the tooling
never lists or creates buckets.

Bootstrap intake repairs two slips before saving: a missing scheme becomes
`https://`, and a path equal to the bucket name is removed (R2's dashboard shows
the S3 URL that way). An explicit `http://` stays, and any other path is refused,
naming the disk or backup repository but never the value. Storage input is
validated before it is saved, so a refused value can be corrected and retried.

Storage namespaces must be empty or end in `/`. Storage collections must use
complete JSON values or their `_FILE` aliases.
Consolidate nested environment overrides into those collections before changing
storage. Bootstrap and configuration changes persist candidate binding reservations
before publishing `app.env`. After proving the selected configuration, they record
its bindings and clear the reservation. Reconciliation selects the proven old or
new revision; an unresolved change retains both old bindings and candidate
reservations. App credentials must be scoped to the app bucket;
provider policy and denial probes establish that isolation. The framework does
not create buckets or edit provider policies. Local upload staging beneath
`/var/lib/foundry/apps/<name>/uploads` is not the durable attachment store.

R2 attachment cleanup relies on write-once original and variant-generation keys,
conditional creation, writer fencing, and durable retirement before unconditional
delete. R2 is not advertised as supporting conditional delete. Existing variant
keys remain readable. See [attachments](attachments.md) for the storage lifecycle.
An S3 stand-in object probe proves transport behavior. Real attachment round
trips and R2 credential isolation still require application acceptance.

## Backups

Backups cover PostgreSQL data (full/differential backups and WAL for point-in-time
recovery) and an age-encrypted key/config archive. Attachments remain in the
application's object storage and are not copied.
Coordinated queue operations also create
[instance Redis snapshots](#redis-snapshot-evidence) in the same backup bucket.
The database backup schedule and `ACTION=backup-run` do not capture Redis queues.

pgBackRest provides continuous WAL archiving for the dedicated Foundry cluster.
WAL is uploaded about once a minute while there is activity, so losing a server
loses about the last minute of commits when archiving is healthy. On an idle
database nothing new is uploaded because nothing changed. A tool upgrade applies
this timeout to existing servers with a reload, not a restart. Each upload adds
one object to the backup bucket, retained for the point-in-time recovery window.

Its S3 repository uses HTTPS with certificate verification, a distinct backup
bucket, and a server-generated repository encryption passphrase. The private
repository binding is `/etc/foundry/backup/repository.json`. pgBackRest reads its
credentials from `/etc/foundry/pgbackrest/foundry.conf`, owned by `foundry-postgres` with
mode 0600. Every instance sharing
the cluster must use the same repository and preserve its passphrase.

The installed schedule runs a full database backup on Sunday at 02:00 UTC and
a differential backup daily at 05:00 UTC. If no base backup exists, the first
differential request takes a full backup. Each database run also archives each
configured instance's keys and configuration. These two timers use persistence
and up to ten minutes of jitter. Retention is cluster-wide and time-based. `repo1-retention-full-type=time` and
`repo1-retention-full=35` retain full backups for 35 days, with their differential
backups and required WAL, to cover at least 28 days of PITR. Per-instance bundle
counts never control expiration. Profiles may omit `backup_policy` or select
`interval_hours: 24`, `full_retention_days: 35`, and `pitr_days: 28`.

Bootstrap writes `APP__FEATURES__ATTACHMENTS__CONFIG__CLEANUP_GRACE=72h` with the
storage configuration; raise it through a configuration change when needed.
A database restore older than the application's attachment cleanup grace may
reference objects the application has already removed. Backups do not read or
require this setting.

`ACTION=backup-run` creates a completed receipt after the database backup and
age-encrypted key/config archive succeed. The receipt contains `backup_id`,
`pgbackrest_label`, `key_archive`, completion time, and configuration revision;
a recovery bundle also records its named `restore_point`. The archive contains
the instance configuration and referenced files, including active and retained
keys and repository recovery material. New archives also contain
`recovery/instance.json`: the nonsecret registry entry with topology, profile,
allocations excluding UID and GID, and `release_commit` for the running release.
Older archives remain recoverable using
the application's `deploy/topology.json` and the HTTP port in `app.env`. Only public age recipients are stored
on the host. `rclone` uploads and verifies only the encrypted key/config archive.
`ACTION=backup-configure` accepts `backup_s3` and `age_recipients`; application
storage credentials remain part of bootstrap runtime configuration.

Before pending migrations, the lifecycle checks the latest full or differential
backup. If it is less than 24 hours old, the lifecycle reuses it. Otherwise it
takes a differential, or a full when the repository has no base. It then creates
a named PostgreSQL restore point and forces WAL archival. Migrations do not
create an extra full backup each time.

A bundle records its database point and key archive separately under its
operation UUID. Completed phases survive retries. Unknown outcomes require
explicit reconciliation. The host report considers a completed bundle fresh
when its database point is younger than the 24-hour backup interval plus ten
minutes of timer jitter and the 30-minute backup deadline, and its configuration
revision matches the current revision.
A new key archive cannot make an old database point fresh.

Each scheduled database run has a separate journal beneath
`/etc/foundry/backup/operations/`. A failed bundle retains its evidence and does
not block the next scheduled database backup or other instances' components.
Instance journals live beneath `/var/lib/foundry/private/instances/<name>/backups/`.
Use the registered bundle's `lib/backup.py` as root for explicit reconciliation:

```sh
python3 -I /usr/local/lib/foundry/current/lib/backup.py reconcile INSTANCE_UUID OPERATION_UUID
python3 -I /usr/local/lib/foundry/current/lib/backup.py reconcile-scheduled OPERATION_UUID
```

If the database outcome is unknown, inspect pgBackRest's inventory and append the
original completed label to the command. Reconciliation checks that label against
the recorded inventory and operation time window, then checks WAL availability.
It never takes another database backup under the unknown operation. A named
restore point with an unknown outcome remains unresolved because recreating the
same name would make recovery ambiguous. After investigating an unprovable outcome, append `--abandon` instead of a
label to preserve its evidence and prevent further reuse. Abandonment does not
claim rollback or absence of uploaded data. Start a new recovery operation. Unknown key or object uploads are copied and checked
again only during explicit reconciliation. Existing completed components remain
selected.

### Repository credential custody

The Foundry cluster and pgBackRest run as the dedicated `foundry-postgres` OS
account. PostgreSQL's `archive_command` invokes `pgbackrest archive-push` directly
as that account with the Foundry configuration, as `pgbackrest check` requires.
The obsolete `/etc/sudoers.d/foundry-backup-archive` bridge is removed before the
direct command is selected. If rebinding fails, reconcile the pending upgrade
to restore WAL archival. The shared distro `postgres` account receives no
repository credential access.

Repository credentials and the encryption passphrase stay in private files.
They are absent from process arguments and environments. Restore drills use the
same pgBackRest configuration. Root-run rclone commands use temporary mode-0600
configuration files. The encrypted off-host key archive includes the repository
recovery material.

A tool upgrade rebinds the owned backup services and timers and the live
`archive_command`. When backups are configured, its plan lists a PostgreSQL reload
if the archive command changes, the cluster lacks `archive_timeout`, the HBA
policy changes, or its publication is unfinished. The plan and PostgreSQL
preparation share these comparisons; the plan token binds this
effect. It verifies systemd's loaded commands and PostgreSQL's live
setting before clearing the upgrade operation. A failed rebind leaves the upgrade
pending so a retry can complete it.

### PITR and restore drills

`ACTION=restore-drill BACKUP_ID=<completed UUID>` restores into a newly owned
`/var/lib/foundry/postgresql/drills/<drill ID>/` cluster. Optional `TARGET_TIME`
selects a timestamp with timezone for PITR, such as `2026-11-02T07:55:00Z`. Without a timestamp, the drill uses
the receipt's named pre-migration point or its first consistent recovery point.
The drill pauses WAL replay at the selected target and checks the database
catalog. Its PostgreSQL instance listens only on a private Unix socket and uses
peer authentication as `foundry-postgres`; it has no TCP listener or app roles running.

The drill persists its identity, paths, target, and phase before allocation.
Retries with the same backup and target options reattach to that drill. The
restore command and PostgreSQL replay each use a named systemd service. The
drill uses a fresh configuration, HBA, and service unit. It cannot overwrite
live data or start application schedulers. Successful temporary resources are
removed only after both services have stopped, their main PIDs are zero, and
their cgroups contain no processes. Retained or failed drills preserve their
paths and phases for inspection. Interrupted restore allocation can be retried
only after proving process exit for those same owned resources.

Database drill success does not establish business invariants, age decryption,
or an atomic snapshot across database and objects. Recovery acceptance must
retrieve and decrypt the key archive with an operator-held age identity, verify
its members, and replay a timestamp-bounded row change. Keep the release artifact and operator recovery identities independently
recoverable. Use replacement-host recovery below to rebuild a managed server.
The drill does not rebind host identity or change traffic routing.

### Recovering onto a replacement host

Keep the existing project and environment profile. Download and decrypt the key
archive on your Mac, then extract it into a private directory. Keep the age private
identity outside that directory and off every server. Recovery reads the decrypted
files and streams them over SSH stdin. It never requests the age private identity.

Choose a new, empty Ubuntu server with the backup's PostgreSQL major: Ubuntu 24.04
provides PostgreSQL 16, and Ubuntu 26.04 provides PostgreSQL 18. The release must
match the server architecture and the restored schema. `RELEASE` accepts the same
retained version or `build` selection as deployment. In both modes, recovery runs
the selected release's own `migrate:status` and refuses pending migrations,
applied migrations unknown to the release, or changed/incomplete history. It
never runs migrations or seeds. New key archives record the running release's
commit in `recovery/instance.json`; a schema refusal names that commit. For old
archives, the commit is reported as unknown. After takeover, upgrade through
normal `make deploy`, which takes its own recovery point before migrations.

To prove the backup while the old server remains alive, run:

```sh
ENVIRONMENT=production ACTION=recover \
  RECOVER_HOST=replacement.example.com RECOVER_MODE=rehearsal \
  FOUNDRY_RECOVERY_DIR=/absolute/private/decrypted-backup \
  RELEASE=<retained-version> make bootstrap
```

`RECOVER_MODE` is required and has no default. Personal SSH prompts work as in
bootstrap. The replacement uses the profile's SSH port. Optional `TARGET_TIME`
selects a timestamp with timezone, such as `2026-11-03T07:55:00Z`. Without a target,
recovery selects the latest completed backup and replays available archived WAL.
A time target selects the newest completed backup preceding that time and requires
WAL through the target.

`recover-plan` is read-only. It rejects existing Foundry registries, resources and
accounts, unsupported operating systems, and missing or mismatched recovery
members. The plan binds the source archive, release, profile, mode, target and
framework bundle to a token. Apply first installs only the `pgbackrest` package
and its required dependencies through the existing guarded package installer.
It then uses `info` with a private temporary configuration to inspect the repository.

If no completed backup exists, or its PostgreSQL major differs from the target,
apply refuses before creating accounts, a registry, databases or other services.
It deletes the temporary credentials and records the refusal in
`/var/lib/foundry-recovery/operation.json`. The package transaction and journal
remain. Recreate the server on the matching Ubuntu release before trying again.

A **rehearsal** restores the database with archiving off, creates new local PKI and
Redis, and starts only HTTP on localhost. It does not issue a public certificate,
install backup timers, start workers or the scheduler, or write to the backup
repository. Its registry state is `rehearsal`; deploy and other mutations refuse
that state. The local profile keeps its original host. A rehearsal cannot be
promoted to takeover. Recreate the server to take over.

Before a **takeover**, stop the old server. Two servers archiving to the same
repository can corrupt recovery. Then run:

```sh
ENVIRONMENT=production ACTION=recover \
  RECOVER_HOST=replacement.example.com RECOVER_MODE=takeover \
  CONFIRM_OLD_SERVER_GONE=yes \
  FOUNDRY_RECOVERY_DIR=/absolute/private/decrypted-backup \
  RELEASE=<retained-version> make bootstrap
```

Without `CONFIRM_OLD_SERVER_GONE=yes`, takeover requires the typed confirmation
`OLD SERVER STOPPED`. `CONFIRM=yes` confirms the printed plan only; it does not
replace the old-server confirmation.

Takeover restores and promotes PostgreSQL, enables WAL archiving, and runs
`pgbackrest check`. It rebuilds local PKI, Redis ACLs, role units and nginx, and
issues a certificate using the recovered Cloudflare token. HTTP starts and passes
readiness before workers and the scheduler start. After all roles are ready,
the tool completes a full database backup and an encrypted key archive, then
enables the normal backup timers.

The instance UUID, database and role names, passwords, encryption keys, and
repository cipher passphrase survive recovery. The replacement gets a new host
UUID, cluster ownership token, CA and service certificates. Its registry records
`recovered_from`. On takeover success, the tool updates `host.address` and
`host.host_id` in the local environment profile. **Commit that profile change and
point DNS at the new server.** DNS is the remaining manual traffic-cutover step.

Repeat the same command with the same recovery folder and release to resume an
interrupted operation. Keep `.deploy/recovery/<instance UUID>.json`; it retains
the operation and replacement host UUIDs and the selected release archive. A
retry reuses that archive, including when the original command used `RELEASE=build`.
The marker is removed after confirmed success and the takeover profile update,
so a completed rehearsal can be followed by takeover on a new empty server.
Completed phases stay completed, and readiness is checked again before backup
and completion. An
active restore attaches to its existing systemd unit. A lost restore completion
record or an uncertain backup outcome never triggers an automatic new restore or
backup. Inspect the private recovery journal and the relevant service or backup
journal. Backup uncertainty uses the reconciliation commands documented above.
An unprovable restore requires recreating the target.

Recovery restores PostgreSQL and configuration. Redis starts empty. Attachments
remain in the existing application bucket; a database target older than attachment
cleanup grace can reference removed objects. Readiness and table comparisons do
not prove application-specific invariants, existing ciphertext decryption, or
large-database restore times. Verify those against representative application data
before routing production traffic.

Deployment tool generation 5 adds the queue startup gate and coordinated cutover
and Redis restore actions. Generation 4 added per-role restart and stop requests;
generation 3 introduced replacement-host recovery. The wire protocol remains 2.0. Upgrade existing hosts through
`ACTION=tools-plan` and `ACTION=tools-apply`; normal deployment does not replace
installed host tools. An empty recovery target receives the selected generation
during recovery.

## Host report

`ACTION=report make deploy` runs `foundry-host-report`, which only reads. Every
item in `problems` needs a look. `complete` is false when there are any. Unknown
values are `null`, never zero or false.

| Field | Source | Problem codes |
| --- | --- | --- |
| `host.failed_units` | systemd failed units, at most 32 | `units_failed`, `failed_units_unavailable` |
| `host.network.public_listeners` | TCP ports listening off loopback | `unexpected_public_listener` for anything but 22, 80 and 443; `network_unavailable` |
| `host.network.firewall` | `ufw status`, root only | None: a cloud firewall cannot be seen from the host |
| `host.ssh` | `sshd -T`, root only: password login and root login settings | `ssh_password_login` |
| `host.resources` | Memory and disk | `memory_low` under 10% available; `disk_low` under 10% or 2 GiB available |
| instance `certificate` | TLS handshake on 443 | `certificate_expiring` under 14 days; `certificate_invalid`, `certificate_renewal_unverified` |
| instance `backup` | Backup receipts | `backup_stale`, `backup_failed` |
| instance `queue_compatibility`, `cutover` | Persistent startup admission and operation progress | `queue_start_fenced` when startup permission is closed |

The root-only fields are `null` for a nonroot reader and add no problem.

The report runs only when someone asks for it, and nothing alerts on a failed or missing
backup or certificate renewal. Alerts are [deferred](../../blueprint/25-deferred-extensions.md#backup-and-certificate-failure-alerts).

## Container acceptance

`make deploy-tools-check` runs the tooling's service-free unit tests, and
`make verify` includes it. Container acceptance exercises real services.

The runner accepts an application release archive and the integrated framework
root, with Ubuntu 24.04 or 26.04, arm64 or amd64, and `--foreign` or `--fresh`.
For example, from the framework checkout:

```sh
tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive /absolute/app.tar.gz --framework-root "$PWD" --foreign
```

Run each platform on a native Docker host of that architecture. QEMU user
emulation of amd64 on arm64 cannot complete it: Ubuntu 24.04's `rclone`
crashes in the Go runtime, and Ubuntu 26.04's systemd cannot create unit mount
namespaces.

The archive must implement the console commands used by deployment, declare
scheduler configuration and Redis inventories, and expose readiness. It needs
HTTP and scheduler roles, `.coordination` lease and `.portal-runtime` cache child
namespaces, and a schedule running at least once every 60 seconds. Its successful
handler must emit a JSON `msg` matching `ACCEPTANCE_SCHEDULE_MESSAGE`, whose
default is `acceptance schedule completed`. Fixture identity is generated for
the disposable host. The runner uses VersityGW as an HTTPS S3 stand-in with a
test CA trusted inside that host. The image is pinned to
`versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316`.
The `s3` network alias matches the certificate SAN. The POSIX backend stores
buckets in a dedicated volume. Acceptance creates the application and backup
buckets through the S3 API. The stand-in and clients use region `us-east-1`.
The runner keeps private logs and uses isolated containers and storage. It does
not publish host ports.

The foreign fixture records configuration bytes, PostgreSQL and Redis PIDs,
data, and reachability before bootstrap and compares them after deployment and
recovery. T1 checks verified PostgreSQL TLS success before and after each
cross-instance denial, so an unavailable database cannot satisfy isolation.
Other phases cover scheduler occurrence, exact Redis grants, atomic key/config
changes, rollback replay, interrupted-switch reconciliation, backup freshness,
WAL replay, age recovery, attachment copies, nginx, and certificate renewal.
`acceptance_recover.py` then uses the bootstrapped source as host A and creates
empty hosts B and C. It compares per-table counts and hashes on rehearsal B and
requires the repository object listing to remain unchanged while A is alive.
After stopping A, it checks takeover C, a later deployment, a new backup, and a
restore drill. Completed temporary drills must leave no failed `foundry-restore-*`
unit or journal entry containing `abnormal database system shutdown`.

Synthetic certificates require `FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1`, the Docker
marker, runtime environment `testing`, an `acceptance*` profile environment,
a `.acceptance.invalid` hostname, and private CA files under
`/etc/foundry/acceptance-ca/`. Production profiles refuse the switch.
The fixture exercises owned lineage files, renewal
configuration, timer checks, certificate validation, and nginx reload using a
local CA. Real Cloudflare and Let's Encrypt integration needs separate authorized
provider acceptance.

Python unit tests cover state machines and fault paths. Release-tool tests cover
source packaging. Neither proves Ubuntu package side effects, live TLS/service
behavior, restore correctness, or the application console contract. Those remain
requirements of the lead's container matrix before production use.

## Bundle interfaces

These interfaces are internal to one installed bundle. `contracts.py` owns
identity and path derivation; `topology.instance_units(entry)` owns role names.
Registry and profile inputs must pass their validators. Inspection results are
bounded public data, never secret values or secret-derived hashes.

`postgres.cluster_settings(host, registry)` returns the allocated shared-service
settings. Backup configuration adds its archive command and passes those settings
to `postgres.prepare(host, registry, shared_service_plan=plan)`. PostgreSQL owns
the archive transition journal and cluster reload. Redis key and channel
inventories pass together through the same widen/narrow transition owner.

`server/manage.py` receives the stdin envelope and routes lifecycle, backup, and
upgrade commands under the owning locks. Certificate and backup provisioners
consume the same instance-bound `bootstrap-secrets.json`. Reports project public
state; private operation receipts stay beneath the private instance directory.
Tool inventory requires every runtime module, range file, and service/nginx
template, and the release source policy includes each exceptional asset by exact
path. Removing a required component makes the bundle incomplete before install.
