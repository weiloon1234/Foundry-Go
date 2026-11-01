# Deployment operator contract

Foundry-Go owns the Python deployment tooling under `tools/deploy`. Applications
own their identity, profiles, topology, and nonsecret application configuration.
The bundle implements layout v2, dedicated shared services, role lifecycle,
Cloudflare certificates, and off-host recovery. Container acceptance remains a
release requirement; unit tests do not establish live package or provider behavior.

## Invocation

The current entry point is `tools/deploy/local/cli.py` in the application's pinned
Foundry-Go module. The Go `foundry deploy` command is deferred. An application
Makefile can contain these thin targets:

```makefile
.PHONY: bootstrap deploy release
bootstrap deploy release:
	python3 -B "$$(GOWORK=off go list -m -f '{{.Dir}}' github.com/weiloon1234/Foundry-Go)/tools/deploy/local/cli.py" --app-root "$(CURDIR)" $@
```

`--app-root PATH` selects the application directory. `FOUNDRY_APP_ROOT` supplies
the directory when the argument is absent. The directory must contain `go.mod`.
The CLI never treats its installation directory or the working directory as an
implicit application root. Framework tools remain in the selected module, and
local state remains in the application directory.

| Command or action | Purpose |
| --- | --- |
| `make bootstrap` | Select or create an environment, then inspect or plan preparation |
| `ACTION=inspect make bootstrap` | Inspect public identity and host state |
| `make release` | Build a local release archive |
| `ENVIRONMENT=production ACTION=status make deploy` | Read the selected instance status |
| `ACTION=deploy make deploy` | Select or build a release, then request activation |
| `ACTION=config make deploy` | Submit up to 16 related configuration values together |
| `ACTION=report make deploy` | Read the public host report without sudo |
| `ACTION=sync make deploy` | Show the local profile and topology diff against the registry |
| `ACTION=tools-plan make deploy` | Stage the selected framework bundle and inspect an upgrade plan |
| `ACTION=tools-apply make deploy` | Apply the reviewed tool plan with `PLAN_TOKEN` and `OPERATION` |
| `ACTION=upgrade-resume make deploy` | Resume that same tool operation after interruption |
| `ACTION=backup-configure make deploy` | Reconcile the persisted backup binding and schedules |
| `ACTION=backup-run BACKUP_KIND=full make deploy` | Complete a database, key, and object backup |
| `ACTION=restore-drill BACKUP_ID=<UUID> make deploy` | Restore an isolated PostgreSQL recovery target |

`ENVIRONMENT` selects an existing profile. `RELEASE` selects an exact retained
release version, or `build`. Interactive operations request confirmation after
showing their plan. `CONFIRM=yes` supplies that confirmation in automation.
Upgrade application additionally requires the digest-bound `PLAN_TOKEN` and a
stable UUID in `OPERATION`.

The local tooling uses Python 3.11 or later, Git, Bash, OpenSSH, and Docker with
Buildx for release builds. The inherited builder resolves Go from the app's
`go.mod` and Node from `web/package.json`. It builds `cmd/artisan`, packages
`config/deploy.toml`, and includes public assets and private source maps when
present. Existing `client` and `web` packages use their declared build scripts.
The module's build tools are mounted read-only separately from the app source.
No compiler runs on the server.

SSH requires an established host key and noninteractive authentication. Personal
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

`tools/deploy/protocol.json` owns protocol 2.0 and generation 1. The stable
launcher dispatches to the active immutable bundle. `hello` reports host tool
metadata without an application mutation. Requests include the client's protocol,
framework version, generation, and bundle digest.

A mutation from a newer client returns `host_upgrade_required`. A client below
the supported floor receives `client_too_old`. Generation 1 is the only current
generation. Compatibility adapters are deferred.

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
| `FOUNDRY_SECRET_CONFIG_FILE` | JSON object of 1 through 16 configuration keys and string values for `ACTION=config` |

The client sends these values in the JSON request on SSH stdin under `secrets`.
Values never belong in command arguments, tracked profiles, or release archives.
The server records bootstrap intake as root-only mode 0600
`/etc/foundry/instances/<name>/bootstrap-secrets.json`, bound to the instance UUID.
App-readable runtime values are separate from root-only control credentials.
Retries preserve acknowledged credentials; missing or conflicting credentials
require recovery rather than regeneration.

When encryption input is absent, the server generates the key ID and 32-byte key
together. Key material uses canonical unpadded base64url. The initial previous-key
table is empty. Recovery identities for age remain in the operator's password
manager or offline custody, never on the production host.

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
Capacity uses fixed conservative allocations and a memory sanity check; a
workload-based capacity calculator is deferred.

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
a renewal dry-run with deploy hooks; a certificate alone does not satisfy readiness.

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

The backup copier reads private cloud disks and their static credentials from
the selected, validated runtime configuration revision. Bootstrap storage secrets
do not select the source of later copies. The registry records each disk's bucket
and namespace prefix. Overlapping prefixes across instances are refused, even
when endpoint spellings differ. Within an instance, overlapping aliases must have
identical bindings. The backup repository uses a distinct bucket.

Storage namespaces must be empty or end in `/`. Storage collections must use
complete JSON values or their `_FILE` aliases.
Consolidate nested environment overrides into those collections before changing
storage. Bootstrap and configuration changes persist candidate binding reservations
before publishing `app.env`. After proving the selected configuration, they record
its bindings and clear the reservation. Reconciliation selects the proven old or
new revision; an unresolved change retains both old bindings and candidate
reservations and blocks object copies. Backups use registered runtime storage from
the first bootstrap configuration. App credentials must be scoped to the app bucket;
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

pgBackRest provides continuous WAL archiving for the dedicated Foundry cluster.
Its S3 repository uses HTTPS with certificate verification, a distinct backup
bucket, and a server-generated repository encryption passphrase. The private
repository binding is `/etc/foundry/backup/repository.json`. pgBackRest reads its
credentials from `/etc/foundry/pgbackrest/foundry.conf`, owned by `foundry-postgres` with
mode 0600. Every instance sharing
the cluster must use the same repository and preserve its passphrase.

The installed schedule runs a full database backup on Sunday at 02:00 UTC and
a differential backup daily at 05:00 UTC. If no base backup exists, the first
differential request takes a full backup. A daily 08:00 UTC job also archives
keys and copies attachment objects. Timers use persistence and jitter. Retention
is cluster-wide and time-based. `repo1-retention-full-type=time` and
`repo1-retention-full=35` retain full backups for 35 days, with their differential
backups and required WAL, to cover at least 28 days of PITR. Per-instance bundle
counts never control expiration. Profiles may omit `backup_policy` or select
`interval_hours: 24`, `full_retention_days: 35`, and `pitr_days: 28`.

Bootstrap writes `APP__FEATURES__ATTACHMENTS__CONFIG__CLEANUP_GRACE=72h` with the
storage configuration; raise it through a configuration change when needed.
The deploy tooling requires an explicit duration longer than the object-copy
interval plus timer jitter. Allow additional margin for outages and copy duration.
The application must retain replaced and retired objects for that grace period.

`ACTION=backup-run` creates a completed receipt only after the database backup,
age-encrypted key/config archive, and object copy succeed. The receipt contains
`backup_id`, `pgbackrest_label`, `key_archive`, `objects_prefix`, and completion
time. The archive contains the instance configuration and referenced files,
including active and retained keys and repository recovery material. Only public
age recipients are stored on the host. `rclone copy` preserves deleted source
objects; replaced destination objects move to an object-history prefix. Neither
`objects/` nor object history is pruned in this release, so storage use grows with
retired objects and changes. Each physical binding has its own destination beneath
`objects/<instance UUID>/`; the receipt's `object_copy.bindings` maps sources to
these paths. A storage change keeps the previous copy available.

Before pending migrations, the lifecycle checks the latest full or differential
backup. If it is less than 24 hours old, the lifecycle reuses it. Otherwise it
takes a differential, or a full when the repository has no base. It then creates
a named PostgreSQL restore point and forces WAL archival. Migrations do not
create an extra full backup each time.

A bundle records its database point, key archive, and object copy separately
under its operation UUID. Completed phases survive retries. Unknown outcomes
require explicit reconciliation. A bundle is complete only when both the database
point and successful object copy are newer than `cleanup_grace`. The host report
uses the older component time for freshness and rejects a mismatched runtime
revision. A new object copy cannot make an old database point fresh.

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
`archive_command`. It verifies systemd's loaded commands and PostgreSQL's live
setting before clearing the upgrade operation. A failed rebind leaves the upgrade
pending so a retry can complete it.

### PITR and restore drills

`ACTION=restore-drill BACKUP_ID=<completed UUID>` restores into a newly owned
`/var/lib/postgresql/foundry-drills/<drill ID>/` cluster. Optional `TARGET_TIME`
selects a timestamp with timezone for PITR. Without a timestamp, the drill uses
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
its members, replay a timestamp-bounded row change, and read copied attachment
bytes. Keep the release artifact and operator recovery identities independently
recoverable. Actual disaster recovery and traffic cutover remain explicit
operator procedures; the drill does not rebind host identity.

## Container acceptance

The runner accepts an application release archive and the integrated framework
root, with Ubuntu 24.04 or 26.04, arm64 or amd64, and `--foreign` or `--fresh`.
For example, from the framework checkout:

```sh
tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive /absolute/app.tar.gz --framework-root "$PWD" --foreign
```

The archive must implement the console commands used by deployment, declare
scheduler configuration and Redis inventories, and expose readiness. It needs
HTTP and scheduler roles, `.coordination` lease and `.portal` cache child
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
