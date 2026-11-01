# Deployment operator contract

Foundry-Go owns the Python deployment tooling under `tools/deploy`. Applications
own their identity, profiles, topology, and nonsecret application configuration.
The F0 foundation establishes layout v2 and the interfaces for follow-up lanes.
F2 and F3 provisioning functions still raise `NotImplementedError` where marked.
Production bootstrap requires those implementations and F4 acceptance.

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
  "backup_policy": {"interval_hours": 24, "daily": 7, "weekly": 4, "monthly": 12}
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

## TODO F1: Release lifecycle and configuration

Lane F1 owns `release.py`, `runtime_config.py`, `server/manage.py`, `local/cli.py`,
and provisioning lifecycle integration. Remaining acceptance covers background
stop ordering, HTTP readiness before scheduler startup, live per-role status and
logs, running-executable reconciliation, explicit-target rollback, atomic
configuration changes, encryption key retention, orphan builder recovery, and
production builds from committed `git archive` inputs. F0's inherited build path
is not the completed production-source policy. Native-image requirements must
come from app inspection rather than a profile switch.

## TODO F2: Mixed-host packages, PostgreSQL, and Redis

Lane F2 owns `packages.py`, `postgres.py`, and `redis_acl.py`. Its contract covers
pure-install package simulation, a journaled temporary `policy-rc.d`, and
`NEEDRESTART_SUSPEND=1`. Foundry owns a dedicated PostgreSQL cluster on 55432 and
Redis TLS instance on 56379. Foreign services remain untouched. Redis grants
come from the app's exact inventory and use journaled widening and narrowing.
Production bootstrap remains blocked until these implementations are complete.

## TODO F3: Certificates, edge, and backups

Lane F3 owns `certificates.py`, `edge.py`, and `backup.py`. Its contract covers
Cloudflare DNS-01, existing-lineage checks, renewal validation, and per-site real
IP and origin restrictions. Backups require pgBackRest continuous WAL archiving,
weekly full and daily differential backups, an age-encrypted key and config
archive, object copies through rclone, and an isolated restore drill. Recovery
identities remain in the operator's password manager. Provisioning stubs are not
backup or certificate guarantees.

## TODO F4: Host acceptance and packaging acceptance

F4 lanes own the moved container acceptance files under `tools/deploy/tests`.
They still need adaptation and execution for Ubuntu 24.04 and 26.04 on native
arm64 and emulated amd64. Acceptance must preserve foreign PostgreSQL and Redis
configuration, PIDs, and data, plus foreign nginx site availability through a
graceful reload. MinIO supplies the object-storage fixture. Module packaging,
read-only module-cache invocation, restore drills, scheduler occurrence, and
interrupted lifecycle operations require acceptance before production use.

## Lane interface contract

These are Python interfaces within the bundle, not application APIs. `host` is
`host.Host`; registry/entry/profile arguments are validated dictionaries from
`contracts.py`. Functions never return credentials in public observations.
F2/F3 planning stubs raise `NotImplementedError` before bootstrap mutations.
Their safe entrypoint response is `lane_not_implemented`.

| Module | Interface | Owner |
| --- | --- | --- |
| `contracts` | `identity(project, profile) -> dict`; `instance_paths(name) -> dict`; `validate_registry(value) -> dict`; `atomic_write(path, data, mode=0o644, owner=None, group=None) -> bool` | F0 |
| `topology` | `load_topology(app_root: Path) -> dict`; `validate_topology(value: dict, *, registered: bool = False) -> dict`; `instance_units(entry: dict) -> dict[str, str]` | F0 |
| `packages` | `inspect(host: Host) -> dict`; `install_missing(host: Host, names: Sequence[str]) -> None` | F2 |
| `postgres` | `inspect(host: Host, registry: dict) -> dict`; `prepare(host: Host, registry: dict) -> None`; `provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None` | F2 |
| `redis_acl` | `inspect(host: Host, registry: dict) -> dict`; `prepare(host: Host, registry: dict) -> None`; `provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None` | F2 |
| `redis_acl` | `redis_grant_patterns(entry: dict, inventory: list) -> list[str]`; `redis_channel_patterns(entry: dict, inventory: list) -> list[str]`; `granted_redis_patterns(entry: dict) -> list[str]`; `granted_redis_channels(entry: dict) -> list[str]` | F2 |
| `redis_acl` | `redis_rules(entry: dict, password: str, patterns: Sequence[str] \| None = None, *, channels: Sequence[str] \| None = None) -> list[str]`; `reconcile_redis_grants(host: Host, registry: dict, entry: dict, patterns: Sequence[str], *, channels: Sequence[str] \| None = None) -> bool` | F2 |
| `certificates` | `inspect(host: Host, entry: dict) -> dict`; `certificate_ready(host: Host, entry: dict) -> bool`; `provision(host: Host, registry: dict, entry: dict) -> None` | F3 |
| `edge` | `cloudflare_ranges(directory: Path) -> tuple[date, list[str]]`; `edge_findings(profile: dict, directory: Path) -> list[dict]`; `edge_template(entry: dict, directory: Path) -> dict[str, str]` | F3 |
| `backup` | `configure(host: Host, registry: dict, entry: dict) -> None`; `recovery_point(host: Host, entry: dict, operation_id: str) -> dict`; `archive_keys(host: Host, entry: dict) -> dict`; `copy_objects(host: Host, entry: dict) -> dict`; `restore_drill(host: Host, entry: dict, backup_id: str) -> dict` | F3 |
| `provision` | `bootstrap_plan(host, project, profile, tools_dir=None, *, topology) -> dict`; `bootstrap_apply(host, project, profile, tools_dir, plan_token=None, *, topology) -> dict` | F1 lifecycle |
| `release` | `activate(host, project, profile, archive, checksum) -> dict`; `rollback(host, project, profile) -> dict`; `reconcile(host, project, profile) -> dict`; `config_set(host, project, profile, changes) -> dict`; `services(host, entry) -> dict` | F1 |
| `runtime_config` | `read_credentials(host, entry, missing=False) -> dict \| None`; `runtime_environment(entry, credentials) -> dict`; `validate_runtime_environment(host, entry) -> dict` | F1 |
| `upgrades` | `protocol(tools_dir: Path \| None = None) -> dict`; `protocol_metadata(tools_dir: Path \| None = None) -> dict`; `hello(host) -> dict`; `check_client(host, request: dict, action: str) -> None`; `tool_bundle(tools_dir: Path) -> tuple[Path, list[Path], str]` | F0 |
| `upgrades` | `tools_upgrade(host, registry: dict, tools_dir: Path) -> dict \| None`; `verify_tools(host, registry: dict, tools_dir: Path, allow_upgrade: bool = False) -> tuple[Path, list[Path], str]`; `install_tools(host, registry: dict, tools_dir: Path, allow_upgrade: bool = False, operation_id: str \| None = None) -> dict` | F0 |
| `upgrades` | `plan_upgrade(host, tools_dir: Path) -> dict`; `apply_upgrade(host, tools_dir: Path, plan_token: str, operation_id: str) -> dict` | F0 |
| `server/manage` | `execute(host, action, request) -> dict` | F1 |
| `local/cli` | `resolve_app_root(argument: str \| None) -> Path`; `bootstrap(project, profile, settings, fresh)`; `build(project, profile) -> Path`; `deploy(project, profile, settings)` | F1 |

`topology` snapshots carry `schema`, `roles`, and a canonical `sha256`. Files on
the application side contain only `schema` and `roles`. Package inspection
returns a `missing` list. PostgreSQL inspection must prove `state`, `owner`,
`cluster`, `port`, and `ownership_token` before role provisioning. Planning
observations must be deterministic, JSON-serializable public data: their bytes
are included in the approval digest. Lane F2 records the exact PostgreSQL unit
under `shared.postgresql.unit`; unit rendering consumes that allocation.

Redis registry grants hold literal prefixes. The ACL API uses suffix-wildcard
patterns derived from those exact prefixes, with optional separate channel
patterns. Credential-derived hashes and interrupted grant targets live in
`redis-acl.json` under the private instance path. F1 must integrate both key and
channel inventories when completing activation and configuration transitions.
