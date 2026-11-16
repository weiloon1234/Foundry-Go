# Host capabilities

Foundry-Go v0.32.0 completes B24 release 1: deploy an application and
single-instance custom services on a generation-13 Foundry host. It installs declared distribution packages, verifies
pinned artifacts, prepares service-local configuration and bundle views, and
starts roles through admission in readiness order. Applications receive typed
bindings to private endpoints and executables. Public TCP uses nginx stream and
source-filtered UFW rules; opening follows whole-set health verification. Restart,
boot admission, reports, logs and whole-instance stop are integrated.

This guide describes topology schema 2 and profile schema 3. Existing topology-1
applications and generation 9–12 hosts keep their existing behavior. Upgrade tools
before using release 1 capabilities. The v0.31.0 generation-12 preview remains
supported by its installed tools. Enablement adoption and release 1 transitions set a sticky
generation-13 client floor, including after application rollback. The application
declarations and existing binding API remain compatible. Configuration templates
and public listener bindings, added in v0.33.0, require generation 14 and set a
sticky generation-14 client floor, including after rollback to a release without
either capability. Unix listeners and endpoint bindings require generation 15 and
set a sticky generation-15 floor. Upgrade both the host tools and deploy client
before using them. Role loopback confinement and wildcard private listeners require
generation 16 on both client and host, with the same sticky rollback/recovery floor.
Outbound PROXY protocol requires generation 18 on both client and host. Declaring
it sets a sticky generation-18 floor, retained after rollback and recovery even
when the selected release no longer declares it.
Managed Cloudflare DNS requires generation 18 on both client and host. Selecting
managed mode keeps that floor after rollback or removal of the last managed listener.
Numbered copies, copy bindings, and copy-scoped secrets share generation 18.
Config trees require generation 19 on both peers, with the same sticky floor.
Tools upgrades advance and verify the boot coordinator before publishing any of
these new registry or profile shapes. A stale pin refuses application-state writes.
See the [deployment guide](deployment.md) for SSH setup, credentials, project
identity, HTTP certificates and the ordinary application release layout.

<a id="agent-entry"></a>
## Agent entry

- [Check release 1 capabilities and limitations](#recipe-preview)
- [Declare the app and relay](#recipe-declarations)
- [Connect a Wayland compositor and consumer](#recipe-unix-sockets)
- [Load application bindings](#recipe-bindings)
- [Prepare and operate the host](#recipe-host)

<a id="recipe-preview"></a>
## Release 1 capabilities and limitations

Feature execution fails explicitly when an unsupported operation is requested.
It does not fall back to the legacy lifecycle. Release 1 lifts the v0.31.0
refusals for service-secret intake/delivery, configuration changes and holds,
rollback to feature or legacy releases, release reactivation, maintenance down/up,
backup configuration/run, restore drills and replacement-host recovery. The
following limitations remain; unsupported lifecycle operations return
`capability_unavailable`.

| Remaining limitation | Consequence |
| --- | --- |
| Encryption key rotation | The dedicated `keys rotate` action remains unsupported for feature instances. Generic edits cannot change the keyring contract. |
| Changed reprovisioning of a selected instance | Changes to its profile, package requirements or owned network/boot files refuse. Unchanged re-bootstrap is supported. |
| Newly required packages at activation | Bootstrap installs packages; deployment cannot silently add missing packages. |
| Named lifecycle operations | `ROLE`/`COPY` scope restart, stop and logs; dependency and maintenance checks still apply. |
| Automatic reconciliation of interrupted lifecycle operations | Do not assume an interrupted operation rolled back. Inspect status and retained cleanup evidence. Unresolved cleanup blocks later starts. |
| Bundle ABI without package-version evidence | Bundle consumers must use package executables with one matching observed service package version. An artifact executable cannot supply this ABI proof. |
| Feature release/artifact pruning and helper-bundle pruning | Explicit feature pruning refuses. Automatic helper retention keeps every helper bundle and upgrade journal with a capability diagnostic. Invocation/view limits still apply; reaching a limit refuses further starts. |
| Host capacity changes involving features | Capacity changes are unsupported for feature instances. Legacy neighbors retain their existing supported operations. |

### Operator commands

`make bootstrap ACTION=inspect` reads the host through staged candidate tools,
including before its first registry exists. `ACTION=prepare` then reviews and
applies bootstrap. `make release` builds locally; `ACTION=deploy` reviews and
activates that archive. Tool generation remains 18.

“Supported” below means the feature owner handles the operation; ordinary identity,
plan, readiness and capability-generation checks still apply. On a fresh host,
instance operations require `ACTION=prepare` first; release-dependent operations
also require a selected deployment. Tool upgrades require an installed registry.
Recovery uses its separate empty replacement-host contract, not an existing live
instance. `hello` returns before the client capability gate.

| Server actions (local operator path) | Fresh host | Existing feature instance | Evidence in `tools/deploy/tests` |
| --- | --- | --- | --- |
| `hello`, `inspect` | Supported; no registry required | Supported, read-only | `test_operator_feature_path.py` |
| `bootstrap-plan`, `bootstrap-apply` | Supported | Unchanged preparation supported; changed selected declarations refuse | `test_operator_feature_path.py`, `test_feature_wiring.py` |
| `release-plan`, `activate` (`deploy`) | Prepare first | Supported, reviewed feature transition | `test_operator_feature_path.py`, `acceptance_operator.py` |
| `status`, `logs`, `report`, `sync`, `config-keys` | Prepare first (`report` can inspect an empty host directly) | Supported | `test_operator_feature_path.py`, `test_feature_wiring.py`, `acceptance_operator.py` |
| `rollback`, `restart`, `stop`, `down`, `up`, `config-set` | Prepare/deploy first | Supported, scoped lifecycle/configuration owners | `test_feature_wiring.py`, `test_feature_copies.py`, `test_feature_maintenance.py` |
| `backup-configure`, `backup-run`, `restore-drill` | Prepare first; backup/drill need a deployment/backup | Supported by feature backup/recovery owners | `test_feature_recovery.py`, `acceptance_feature_recovery.py` |
| `recover-plan`, `recover-apply` | Supported with recovery material | Replacement-host recovery only | `test_feature_recovery.py`, `acceptance_feature_recovery.py` |
| `tools-plan`, `tools-apply`, `upgrade-resume` | Prepare first | Supported by upgrade protocol | `test_feature_wiring.py` |
| `capacity-plan`, `capacity-apply` | Refused | No feature admission transition for resizing; retain allocation, inspect `status` | `test_operator_feature_path.py` |
| `keys-rotate` | Refused | No feature keyring transition; retain keyring, use `config` for service secrets only | `test_operator_feature_path.py` |
| `backup-keys-rotate`, `backup-recipients-rotate` | Refused | No reviewed feature recovery-archive rotation; retain credentials/recipients, use `backup-run` with current settings | `test_operator_feature_path.py` |
| `dns-token-rotate` | Refused | No coordinated managed-record credential rotation; retain token, inspect DNS evidence in `status` | `test_operator_feature_path.py` |
| `reconcile` | Refused | No automatic interrupted-operation reconciliation; inspect `status`/`report` and retained cleanup evidence before recovery | `test_operator_feature_path.py` |

Deliberate refusals return `capability_unavailable` before mutation. The exhaustive
server-action regression checks both fresh and registered feature gates; the
operator acceptance phase runs bootstrap inspect/prepare, reviewed deployment,
status, logs and report through the real CLI and installed launcher, on its own
fresh container. Existing direct-request acceptance phases remain separate.

Service secrets use the existing private configuration intake. Declare names in
`secrets` and map service `_FILE` variables through `secret_files`; explicit
`secret_file` arguments and per-start configuration references also receive paths.
The [secret template mode](#secret-configuration-templates) can place their values
inside private prepared files.
Supply `FOUNDRY_SERVICE_SECRET__<UPPERCASE_NAME>` keys through hidden input or
`FOUNDRY_SECRET_CONFIG_FILE`. Hyphens in names become underscores. SSH sends values
on stdin. Unknown names, namespace collisions and simultaneous value/`_FILE`
forms are rejected. Files are root-owned, mode 0640, with the instance group.

Deploy stages the archive and obtains a `release-plan` before confirmation. Newly
required secrets are collected and snapshotted privately during review, before
any service stops. The plan binds an operation UUID, archive and deployment,
current configuration revision, allocations, enablement and host/network
observations; changed inputs invalidate it. Public plans expose opaque intent IDs
and redacted key names, never values or their hashes.

`ACTION=config` rotates service secrets through a configuration revision and the
existing private backup owner. It restarts affected enabled, running services and
any consumers whose endpoint bindings change. Unchanged services retain their
processes and exposure; deliberately stopped services stay stopped. An interrupted
publication remains pending for reconciliation. Do not infer that old credentials
were restored from an unsuccessful operation.

Rollback and reactivation validate the target release against **current** private
configuration and the complete current keyring before withdrawing ingress. They
never restore a historical secret snapshot, migrate backwards or run seeders.
Fresh runtime allocations keep old nginx references away from the target code.
Surviving roles retain actual systemd enablement. Removed roles retain enablement
evidence for later rollback; without that evidence they remain disabled, along
with dependent roles, and the plan reports the resulting selection.

Every newly activated release retains its complete validated topology and profile
under the release state owner, keyed by archive digest and topology digest.
Rollback uses that target-owned record. For older releases, provide
`ROLLBACK_TOPOLOGY_FILE=/path/to/topology.json` when planning rollback. The server
requires its digest to match the target manifest's `topology_sha256`; it never
uses the current topology as a substitute. If the original archive digest was
never recorded, the imported declaration is explicitly keyed by retained manifest
digest, without claiming an archive digest. Missing or mismatched declarations
refuse before stopping services.

Application configuration changes use the application's `config:inspect` and
`config:check`, including private file aliases, storage validation and Redis grant
reconciliation. A known published revision whose startup fails is `failed_applied`.
After independent cleanup proves closure, a corrective revision can proceed;
ordinary starts remain held meanwhile. Unknown publication or cleanup outcomes
still require reconciliation. Rollback to legacy uses its schema-1 runtime
descriptor and current credentials while retaining sticky feature admission and
capable-client requirements. Service secrets remain private for later roll-forward.

Current tools keep feature workload units enabled after startup and read systemd
enablement for committed admission, restart selection, and reporting. Systemd
starts enabled units at boot with the declared `After=` ordering. The exposure
coordinator verifies the enabled, unheld set before opening traffic. A declared
`requires` dependency does not add systemd `Wants=` or `Requires=` links that could
start a disabled unit.

The next privileged instance action migrates a committed v0.31.0 preview by
enabling its running units without starting anything. It does not enable stopped
units. `ACTION=status` includes the migration receipt. A pending deployment must
be reconciled before that migration. Selective secret rotations preserve unchanged
units and their exposure. Boot coordinators validate their per-role generation and
load the current whole-instance selection. During a pending transition, retention
allows only the exact existing live workload to continue. A replacement invocation
needs a validation permit covering its unit and addresses; retaining an earlier
launch origin does not grant permission to start.

| Declaration outside release 1 | Later capability |
| --- | --- |
| Supervising wrappers | B24 release 3 |
| Outbound PROXY v2 and inbound PROXY to nginx | Unsupported; outbound v1 is available with generation 18 |
| UDP listeners | Planned for B24 release 2; unsupported in release 1 |
| Artifact formats other than `file` and `tar.gz` | Unsupported format; no later numbered release is promised |

These declaration failures use schema/capability validation errors rather than
silently dropping fields. In particular, plain TCP **is** supported: declare
`tls: "none"` explicitly and acknowledge application authentication.

## Backup and replacement-host recovery

The backup owner archives PostgreSQL and the age-encrypted configuration tree.
Feature archives also retain the selected deployment, resolution, selection and
release declaration in `recovery/instance.json`. The canonical archived `app.env`
comes from the selected runtime resolution, including its effective HTTP allocation.
Recovery requires the environment, allocation metadata and resolution to agree;
the bootstrap environment does not override the selected port. Declared service-secret snapshots
must follow the configuration owner's contract beneath the instance configuration
tree: current values are root-owned 0640 for the isolated instance group, and the
secret index stays 0600. Recovery restores this access before runtime and storage
readers run, while leaving unreferenced historical snapshots private.
The archive reader checks their paths and the descriptor's identities. Immutable
configuration sources, binaries and artifacts still come from the retained release
archive. Keep that exact archive available off-host.

Backups record systemd's actual enabled roles, rather than the registry's cached
observation. Takeover preserves that enabled set and applicable maintenance holds;
held roles and their dependents stay stopped. An empty enabled set stays stopped.

A bundle's `status: complete` means its supported backup components succeeded.
`coverage.complete_instance_coverage` separately describes service-state coverage:

- `disposable` state is excluded and does not block coverage.
- `local-unprotected` produces `service_state_unprotected`, naming the role, and
  blocks complete coverage. Takeover refuses these roles until an explicit local
  state restoration contract exists. Acknowledging the state policy is not proof
  of restoration.
- `external` records its declared `recovery_owner`. Naming the owner delegates
  recovery of that state; it does not claim Foundry copied or restored it. Older
  declarations without an owner retain `service_state_external_owner_required`
  and cannot claim complete coverage.

Configuration-only receipts never claim complete instance coverage. Database
restore drills use an isolated PostgreSQL Unix socket, start no custom services,
and open no TCP or UDP listener. Their verification does not claim service-state
recovery, key decryption or application invariants.

Recovery rehearsal selects HTTP and its prerequisite closure. Custom roles outside
that closure remain stopped. An unsafe prerequisite refuses with
`recovery_rehearsal_dependency` and its role name. A custom HTTP prerequisite must
declare `recovery_rehearsal_safe: true`; the default is false. Built-in background
dependencies remain refused. Each selected custom role receives systemd
`IPAddressDeny=any` and `IPAddressAllow=localhost`. Its fixed runtime helper reads
back the effective unit policy and verifies attached ingress and egress cgroup BPF
programs before dispatch. Missing support, permissions or matching policy refuses
with `rehearsal_isolation`. Only IPv4/IPv6 loopback is allowed; public routes stay
closed. HTTP retains its existing rehearsal behaviour, and legacy rehearsal is
unchanged. The systemd policy follows the
[IP filtering contract](https://www.freedesktop.org/software/systemd/man/255/systemd.resource-control.html#IPAddressAllow=).

The recovery owner accepts the existing typed `RecoveryPublicBinding` through its
`recovery_public_binding` argument. It requires every source public listener,
unchanged hostname, port, transport and address families, explicit new addresses,
the source binding digest, source and target identities, deployment digest and
operation UUID. Rehearsal rejects this argument. The source descriptor remains
immutable; the effective replacement profile is separate, token-bound evidence.
For external DNS, after confirmed old-server stop and private takeover health, recovery emits exact
DNS-only A/AAAA instructions. `external_pending` and `external_observed` are
separate from host readiness. Recursive answers never prove provider ownership or
complete traffic cutover. Managed DNS follows the
[owned record cutover contract](#managed-cloudflare-dns).

Replacement recovery verifies the original archive/commit, installs its
packages and artifacts, proves the graph stopped with ingress closed, and restores
exact archived enablement and maintenance holds. Activation never migrates or
seeds restored data. It validates the schema and current credentials, then starts
the selected graph with admission checks and independent failed-start cleanup.
An empty enabled set stays stopped. Repeated completion checks freshly verify
release/configuration identity, process admission, private health and exposure.
An interrupted activation keeps its cleanup obligations and refuses blind replay.

For feature takeover with public listeners, set
`FOUNDRY_RECOVERY_PUBLIC_BINDING_FILE` to a JSON file containing the complete
`RecoveryPublicBinding` above. The client uses its operation and target-host UUIDs,
validates it against the archived metadata, and sends the identical object to plan
and apply. Resume with the same file and inputs. Successful takeover persists the
server's validated effective `profile_update`; rehearsal never saves a profile.
The original declaration stays in the archive, including after later backups.

The [recovery handoff](../../tools/deploy/tests/RECOVERY_HANDOFF.md) describes the
callback interfaces and the opt-in container acceptance scenario.

<a id="recipe-declarations"></a>
## Declaration ownership

| File | Owns |
| --- | --- |
| `deploy/project.json` | Schema-1 project UUID and slug; retain this identity across environments. |
| `deploy/topology.json` | Schema-2 role names, commands, packages, artifact pins, private listener intent, readiness, dependencies and binding names. |
| `deploy/environments/production.json` | Schema-3 host/instance identity, placement, public ports, source CIDRs, DNS intent and existing environment settings. |
| `deploy/files/` | Nonsecret configuration templates referenced by topology. |
| Application source | Binding consumption, HTTP authorization callback and business authorization. |
| Operator's DNS provider | External records remain operator-owned. Managed records use explicit Cloudflare zone and per-record ownership. |

The application release builder includes the topology and verified artifact
payloads. Operators supply placement and exposure policy in the profile. Do not
put allocated private addresses, generated role descriptors or host filesystem
paths into either declaration.

### Worked example: app with an authenticated media relay

This example defines an HTTP app plus a generic relay. Supply a Linux relay binary
that implements the argument interface below: a loopback HTTP control server with
`GET /ready`, a PROXY-v1-aware TCP media server, and an HTTP authorization callback client.
The artifact URL, checksum, UUIDs, hostnames, addresses and CIDRs below are
**documentation placeholders**. Replace them with your published artifact and
actual deployment values. This is not a downloadable product configuration.

The relay must send each publish/read authorization decision to
`<authorize-base-url>/internal/media/authorize`, including the client's credential,
requested action and resource. The app authenticates that credential and checks
resource permission. The relay must deny on timeout, malformed response, error or
denial; it must not treat a reachable callback as authorization. Implement this
callback in the app and the corresponding client in the relay. Foundry supplies
the private callback endpoint, not this protocol or business policy. Never log the
forwarded credential. Use a relay readiness check that does not require the app
callback, because the app starts after the relay.

`deploy/topology.json`:

```json
{
  "schema": 2,
  "roles": [
    {
      "name": "http",
      "kind": "builtin",
      "packages": ["jq"],
      "requires": ["relay"],
      "maintenance": "continue",
      "bindings": {
        "relay": {"endpoint": {"role": "relay", "listener": "control", "scheme": "http"}},
        "relay_public": {"public_listener": {"role": "relay", "listener": "ingress"}},
        "json": {"executable": {"package": "jq", "path": "/usr/bin/jq"}}
      }
    },
    {
      "name": "relay",
      "kind": "executable",
      "executable": {"artifact": "relay", "entry": "server"},
      "requires": [],
      "bindings": {
        "authorization": {"endpoint": {"role": "http", "listener": "http", "scheme": "http"}}
      },
      "arguments": [
        {"literal": "--control-address"}, {"listener_address": "control"},
        {"literal": "--control-port"}, {"listener_port": "control"},
        {"literal": "--listen-address"}, {"listener_address": "ingress"},
        {"literal": "--listen-port"}, {"listener_port": "ingress"},
        {"literal": "--authorize-base-url"}, {"binding": "authorization"},
        {"literal": "--authorize-path"}, {"literal": "/internal/media/authorize"}
      ],
      "listeners": {
        "control": {"transport": "tcp", "bind": "loopback"},
        "ingress": {"transport": "tcp", "bind": "public", "tls": "none", "authentication": true, "proxy_protocol": "v1"}
      },
      "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200, "timeout_seconds": 30},
      "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5, "window_seconds": 60, "stop_seconds": 30},
      "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64, "file_descriptors": 1024},
      "maintenance": "continue",
      "state": {"kind": "disposable"}
    }
  ],
  "artifacts": {
    "relay": {
      "version": "1.0.0",
      "format": "tar.gz",
      "platforms": {
        "linux-arm64": {
          "url": "https://downloads.example.org/relay/1.0.0/relay-linux-arm64.tar.gz",
          "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        }
      },
      "executables": {"server": "bin/media-relay"}
    }
  }
}
```

`deploy/environments/production.json`:

```json
{
  "schema": 3,
  "environment": "production",
  "instance_id": "127246a5-4fa2-49e5-8174-c04ac7f1c967",
  "host": {"address": "host.example.org", "port": 22, "host_id": null},
  "runtime_environment": "production",
  "domain": "app.example.org",
  "certbot_email": "ops@example.org",
  "architecture": "arm64",
  "public_env": {},
  "edge_origin_lockdown": false,
  "retention": {"server_releases": 5, "local_releases": 3, "pre_migration_dumps": 5},
  "placement": {"http": 1, "relay": 1},
  "listeners": {
    "relay.ingress": {
      "ports": [8554],
      "source_cidrs": ["203.0.113.0/24"],
      "dns": {
        "mode": "external",
        "hostname": "stream.example.org",
        "public_addresses": ["203.0.113.10"],
        "proxied": false
      }
    }
  }
}
```

The relay listens on **allocated private** addresses and ports, including its
`ingress` backend. Nginx owns the public port 8554 and forwards permitted traffic
to that backend. Do not hard-code 8554 in the relay's argv. The app's `relay`
binding reaches the private control listener; the relay's `authorization` binding
reaches the app's selected private HTTP allocation. Bindings do not create
`requires` edges, so this bidirectional communication has no dependency cycle.
Only the app requires the relay to be ready before startup.

`authentication: true` is an acknowledgement of service responsibility, not an
authentication implementation. Source CIDRs and loopback bindings do not replace
client authorization. `tls: "none"` adds no encryption: use it only where the
protocol/network's confidentiality properties are acceptable. To terminate TLS
at nginx instead, use `tls: "edge"`; the profile still supplies the hostname and
DNS-only addresses, and the existing certificate owner provisions its certificate.
Edge TLS still requires service authentication.

### Outbound PROXY protocol

The example opts its `ingress` listener into `"proxy_protocol": "v1"`. This is a
topology fact: the backend must parse a PROXY header before application bytes.
The profile still owns only the public ports, addresses and source policy. Omit
the field for an ordinary backend; the default sends no header and the backend
sees nginx's connection identity. Role-level, private TCP, Unix and profile-level
listener declarations reject the field. The built-in HTTP role keeps its
forwarded headers; HTTP sites and inbound PROXY from an upstream load balancer
to nginx are outside this capability.

Both `tls: "none"` and `tls: "edge"` support the opt-in. The owned stream server
renders `proxy_protocol on; # v1` only for that listener and still validates the
complete nginx configuration before reload. Nginx sends the client address and
port in a plaintext v1 header to the private backend, after terminating TLS when
enabled. The public client sends its normal protocol, with no PROXY header.
Only v1 is accepted: [nginx's stream directive](https://nginx.org/en/docs/stream/ngx_stream_proxy_module.html#proxy_protocol)
uses `on` for v1; sending v2 requires nginx 1.31.4, beyond the supported Ubuntu
distribution packages. There is no version negotiation or silent downgrade.

Readiness must target a **separate non-PROXY listener**. The example's `control`
listener serves ordinary HTTP readiness. Validation rejects both TCP and HTTP
readiness on a PROXY-enabled listener because local probes do not send the
header. This keeps application health independent of edge framing; a successful
local probe does not prove remote client identity delivery. The opt-in container
acceptance checks that separately for IPv4/IPv6 and plaintext/edge TLS. Reports
include `proxy_protocol` per public listener (`"v1"` or `null`).

Foundry's built-in Go HTTP server does not serve these custom stream backends.
For an application-owned Go TCP server, use an existing parser such as
[`pires/go-proxyproto`](https://github.com/pires/go-proxyproto) at the application's
listener boundary. Require a header, set a finite header-read timeout, and enforce
bounded header parsing. Bind the backend to its allocated loopback endpoint and
trust headers only from the local edge, based on the underlying socket peer,
never the advertised source. Loopback access alone does not authenticate a local
process: untrusted local processes must not be able to reach this backend. Client
addresses remain metadata, not application authentication or authorization. Keep
the control/readiness listener outside the PROXY wrapper.

### Topology field reference

Every topology needs a builtin `http` role. `http`, `scheduler` and `worker` are
reserved builtin names; their framework commands cannot be overridden. Custom
roles have `kind: "executable"` and a nonreserved name. Placement is separate from
role declaration. An executable may reference a pinned artifact export as above,
a package plus an absolute path, or an `application` executable name declared in
`application_executables` with `go_package: "./cmd/<name>"` or platform-specific
paths under `deploy/bin/`.

- **Packages:** role-local lists of literal distribution package names. There are
  no repository additions, version expressions or apt options in this field. A
  package executable binding must name a package in that consuming role's list.
  A declared package executable must resolve to a file the declared package owns
  on the target release, so check `dpkg -S` on the resolved path.
  Tool-only dependencies need no custom service role: the example's `json`
  binding resolves a verified executable without starting a daemon. Framework
  package defaults remain fenced unless their owner explicitly hands them off.

  Package installation refuses configuration changes to any preexisting unit and
  runtime changes to independently managed services, including starts, stops,
  failures and restarts. It ignores runtime changes to numeric `user@` and
  `user-runtime-dir@` session services, transient units, static `Type=dbus`
  services, static run-to-completion oneshots (`RemainAfterExit=no`, such as Ubuntu
  Pro's hook-started `esm-cache.service`) and services triggered only by timers.
  Other static services, including state-holding oneshots, may change freely while
  they were inactive; once active, stopping, failing or restarting them still
  refuses installation. Services triggered by a socket or path, such as
  `ssh.service`, `dbus.service` or `systemd-fsckd.service`, may also start or exit
  cleanly while idle, but a restart or failure still refuses; their socket's
  `ActiveState` stays compared. Timer, path, target and socket units ignore only
  `SubState` changes and still compare `ActiveState`. Session and transient units that appear
  or disappear are ignored. A new instance of a template that existed before
  installation, such as `modprobe@sd_mod.service`, is not unowned activation when
  it is not running or is a run-to-completion oneshot. These rules also apply when
  resuming an interrupted package installation.
- **Artifacts:** `version` is a single path component. `platforms` maps
  `linux-amd64` and/or `linux-arm64` to an HTTPS `url` and 64-hex `sha256` of the
  download, or a [release source](#release-carried-artifacts). Declared URLs cannot
  carry credentials, query strings or fragments. Redirect targets may carry signed
  queries, as GitHub release downloads require. Each request preserves the target's
  path and query; every hop still requires public HTTPS, public resolved addresses,
  a verified peer, and no credentials or fragments. Downloads allow at most five
  redirects and verify the payload against the declared SHA-256. Signed queries
  are excluded from origin evidence and errors. Artifact paths use the declared
  digest, so expiring queries do not change artifact identity. Declare
  the selected profile platform for every artifact. An artifact version cannot
  be reused with changed bytes. `executables` maps export names to relative
  members. `file` exports exactly one executable; `tar.gz` can also export
  `bundles`, a map of names to relative directories. Optional `modules` names
  members beneath those directories. Bundles require `provenance` with
  `source_revision`, `build_inputs`, `license` and `service_abi` strings; release 1
  requires `service_abi` to match the observed consuming package version.
- **Bundle views:** a role's `artifact_mounts` maps local names to
  `{"artifact":"assets","bundle":"templates","area":"config","path":"templates"}`
  (or area `data`). A binding `{"bundle":"<mount-name>"}` or argv
  `{"bundle":"<mount-name>"}` selects its protected service-local view. These are
  not arbitrary host mounts. The package-version ABI restriction above applies.
- **Argv:** `arguments` is a list of single-tag objects, each one argument; it is
  not a shell command. `literal` does not interpolate variables. `listener_address`
  and `listener_port` refer to that role's listener. `binding` names a declared
  binding and supplies an executable path, bundle path, TCP/HTTP endpoint URL or Unix
  socket path. `config`
  names a declared configuration. `state_path` and `runtime_path` contain relative
  paths; `config_root`, `data_root` and `copy_number` use the value `self`
  (the only deployable copy is currently 1).
  `secret_file` supplies a private file path, never secret bytes.
  Maximum 128 arguments, 32 KiB combined values, 4096 bytes per literal.
- **Listeners:** TCP uses `bind: "loopback"` or `"public"`. Loopback listeners
  omit `tls` and `authentication`. Public listeners require `tls: "edge"` or
  `"none"` and `authentication: true`. A public declaration still uses a private
  service backend. Its public address is owned by the profile and nginx.
  Public TCP listeners may opt into `proxy_protocol: "v1"` as described above.
  Unix listeners use `{"transport":"unix","socket":"wl0"}` and are always private;
  they have no `bind`, `tls` or `authentication` fields or profile listener entry.
- **Readiness:** one local `tcp`, `http` or `unix` probe naming a declared listener
  of the matching transport. Unix readiness connects to its declared path socket.
  `timeout_seconds` is 1–300 (default 30). HTTP requires a local `path` without
  query/fragment and a 200–299 `status`; optional `body` is at most 1024 bytes.
  TCP and Unix probes have no HTTP fields. Command and remote probes are not supported.
  A PROXY-enabled public listener requires readiness on a separate non-PROXY listener.
  Opening also checks the complete enabled set's identities, probes and stability.
- **Restart:** required `policy` (`on-failure`, `always`, `no`), `delay_seconds`
  (1–300), and `burst` (1–20). Optional `window_seconds` is 1–3600 (default 60);
  `stop_seconds` is 1–300 (default 30). Automatic starts still pass admission.
- **Limits:** required `memory_bytes` (1 MiB–2^50), `cpu_percent` (1–6400), and
  `tasks` (1–65536); optional `file_descriptors` (16–1048576, default 1024).
  CPU 100 means one CPU's quota. Units also receive framework-owned hardening;
  declarations cannot inject systemd directives or a shell wrapper.
- **State:** required `kind` is `disposable`, `external`, or `local-unprotected`.
  The last additionally requires `acknowledged: true`; the others omit it.
  Only `external` accepts `recovery_owner`, a public label of at most 256 UTF-8
  bytes, with no controls or surrounding whitespace. Omission preserves older
  declarations but blocks a complete-coverage claim. Null and empty labels refuse.
  This declaration does not turn service-local state into a backup guarantee.
- **Recovery rehearsal:** custom executable roles may declare the strict boolean
  `recovery_rehearsal_safe`, default false. It allows planning an HTTP prerequisite
  under isolation; it neither starts unrelated roles nor authorizes public exposure.
- **Maintenance:** `continue` or `stop`; the existing `serve` spelling remains
  accepted for compatibility. HTTP continues by default; other feature roles stop.
  HTTP must continue. Every `requires` prerequisite and endpoint-binding provider
  of a continuing role must also continue. Invalid graphs name the consumer,
  provider and binding before deployment. Endpoint bindings do not add startup
  ordering or implicitly start a disabled provider.
  Executable bindings never create maintenance prerequisites: they name installed
  files (a package path, an artifact entry or an application executable), and
  stopping a role for maintenance does not remove or change those files.
  Feature `down` closes exposure and stops only roles declared `stop`; HTTP and
  continuing services retain their processes and exposure. Root-owned persistent
  per-copy markers prevent held units from starting after reboot or automatic
  restart. Actual systemd enablement remains unchanged. `up` restores the remembered
  running copies that are still declared and enabled; a deliberate disable wins.
  Legacy built-in worker/scheduler application pause behavior remains unchanged.
- **Dependencies:** `requires` names role prerequisites and must be acyclic.
  Defaults are `[]`, except scheduler/worker default to `["http"]`. Enabled roles
  require enabled prerequisites. Executable and endpoint bindings resolve values;
  they do not implicitly start a service or add a dependency edge. Unix endpoint
  consumers must explicitly include the provider in `requires`.
- **Bindings:** each name selects `{"endpoint":{"role":"relay","listener":"control","scheme":"http"}}`,
  `{"executable":{"package":"jq","path":"/usr/bin/jq"}}`,
  an artifact/application executable reference under `executable`, or a bundle
  reference. Endpoint schemes are `http`, `tcp` and `unix`; builtin HTTP's listener name
  is `http`. An enabled consumer cannot bind an unplaced endpoint producer.
  `{"public_listener":{"role":"relay","listener":"ingress"}}` supplies a
  public listener record through the runtime descriptor. It has no implicit argv
  or environment conversion and requires generation 14.

Custom `configs` map names to either a single-file declaration or a `files` map
under one config directory. Each file declares `source` under `deploy/files/` and `mode`:
`immutable`, `seed-once`, `recreate`, `rendered-json`, or `rendered-template`. Per-start preparation
rebuilds `recreate` files; `seed-once` retains the initial seed across starts.
Only `recreate` and `rendered-json` accept `bindings`, a map from JSON pointers to
typed listener address/port, bundle, binding or copy-number values. Do not insert
secrets here. Custom `environment` is an explicit map of nonsecret typed argv
values; reserved process/application names and `_FILE` names are rejected.
See [secret configuration templates](#secret-configuration-templates) for tree
limits and shared directory environments, and the [OBS config tree](#obs-config-tree)
for a complete example.

<a id="recipe-unix-sockets"></a>
### Unix socket listeners

To connect a headless Wayland compositor to a consumer such as OBS, add this role
to your topology's `roles` and set `placement.compositor` to `1` in the profile.
Keep the required builtin `http` role. The executable package must be available
on the host's distribution; bootstrap installs declared packages.

```json
{
  "name": "compositor",
  "kind": "executable",
  "packages": ["weston"],
  "executable": {"package": "weston", "path": "/usr/bin/weston"},
  "arguments": [
    {"literal": "--backend=headless"},
    {"literal": "--renderer=pixman"},
    {"literal": "--socket"},
    {"listener_socket_name": "wl0"}
  ],
  "environment": {"XDG_RUNTIME_DIR": {"listener_socket_directory": "wl0"}},
  "listeners": {"wl0": {"transport": "unix", "socket": "wl0"}},
  "readiness": {"kind": "unix", "listener": "wl0", "timeout_seconds": 30},
  "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
  "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
  "maintenance": "continue",
  "state": {"kind": "disposable"}
}
```

Add these fields to the consuming role, retaining its executable, arguments,
configuration, readiness, restart, limits and state declarations. Merge existing
`requires`, `bindings` and `environment` entries. For OBS, prepare its own settings
and control listener separately; this binding supplies its Wayland connection.

```json
{
  "requires": ["compositor"],
  "bindings": {
    "display": {"endpoint": {"role": "compositor", "listener": "wl0", "scheme": "unix"}}
  },
  "environment": {
    "QT_QPA_PLATFORM": {"literal": "wayland"},
    "XDG_RUNTIME_DIR": {"binding_socket_directory": "display"},
    "WAYLAND_DISPLAY": {"binding_socket_name": "display"}
  }
}
```

Both processes run as the instance account. The framework owns
`/run/foundry/services/<instance UUID>/sockets`; its protected parent prevents the
application from replacing that directory. The directory belongs to the instance
account with mode `0700`. Unit staging creates the mount target, and every admitted
start creates or verifies the directory before dispatch. Only roles providing or
binding a Unix listener receive its `ReadWritePaths` entry, including builtin
consumers. `PrivateTmp`, `ProtectSystem` and address-family restrictions stay in
place. Roles in an instance share an account and are not isolated from each other.

The following references work in argv and explicit nonsecret environment entries:

| Provider reference (value is listener name) | Consumer reference (value is binding name) | Result |
| --- | --- | --- |
| `listener_socket_directory` | `binding_socket_directory` | Shared socket directory |
| `listener_socket_name` | `binding_socket_name` | Socket file name, such as `wl0` |
| `listener_socket_path` | `binding_socket_path` | Full absolute socket path |

Declared socket names contain 1–32 ASCII letters, digits, dots, underscores or
hyphens, starting with a letter or digit. `@` is reserved for derived copy suffixes;
a declaration cannot use a name such as `wl0@2`. Names must be unique across the
instance's roles. The same codec validates derived names against their ordinal.
A 32-character name at ordinal 128 produces a 103-byte path, below the enforced
107-byte Unix socket path limit. The suffix stays literal in systemd arguments;
Unix listener inspection compares the full absolute path, including `@N`.
Paths, abstract sockets, slashes, traversal and public Unix bindings are refused.
Unix listeners allocate no TCP ports and create no firewall or nginx rules.
`listener_address` and `listener_port` remain TCP-only.

Before starting a provider, preparation examines only its declared socket names
without following links. It removes a socket only after a bounded connection
attempt reports connection refused. A live socket, uncertain liveness, a regular
file, a directory or a symlink fails the start and is retained for inspection.
Other names are untouched. Both readiness and stale-socket liveness run in a
bounded helper as the instance account. Root reads the helper source from the
verified bundle and passes it to the isolated Python process, so the instance
account does not need access to the bundle directory. The helper opens the directory
and socket leaf without following symlinks, then connects to the pinned socket inode. Readiness
also checks the connected peer's UID and unit cgroup, holding a kernel peer pidfd
to reject exit or PID reuse during inspection. Missing kernel support or uncertain
peer identity fails readiness; there is no privileged connection fallback. The
existing process identity, listener ownership and stability checks still apply.
Tool-driven startup waits for the provider before starting its dependent; boot
ordering and automatic restarts retain the existing dependency semantics.

`deployment.Bindings.Endpoint` returns a Unix endpoint with `Scheme == deployment.Unix`,
`Network() == "unix"`, `Path`, `Directory` and `Name`; `Address` and `Port` are zero.
Its `URL()` is `unix:///...`. Existing TCP/HTTP endpoints retain their address,
port and URL behavior. See the compiled consumer source in
[`hostcapabilities/unix.go`](../../tests/fixtures/consumer/hostcapabilities/unix.go).

Unix endpoint bindings count as maintenance prerequisites. A continuing consumer
cannot depend on a provider declared to stop. Backups retain the declarations and
binding records, but never archive the runtime socket directory or its contents.
Recovery prepares a fresh directory and starts providers through the same readiness
path. The generation-15 floor survives rollback and recovery.

### Secret configuration templates

Use `rendered-template` for a daemon that reads secrets from its own configuration
file. Declare `format` as `ini`, `yaml`, `json`, or `text`. Use `recreate` with the
same `format` when the daemon rewrites the file. Template rendering is a fixed
framework preparation step after start admission. It never executes a shell,
application hook, or template expression.

The optional `path` defaults to `value` and is relative to that config's directory
inside the invocation view. Absolute paths, traversal, and empty path components
are refused. An argv reference such as `{"config":"settings"}` resolves the
declared file. `directory_environment` may be `HOME` or `XDG_CONFIG_HOME`; the
framework sets that variable to this config's directory in both the unit and
workload environment. Duplicate claims and conflicts with explicit role
environment entries refuse. Existing `{"config_root":"self"}` still names the
bundle config view, not this invocation directory.

A config entry can instead declare a `files` map for a program that reads a whole
configuration tree. The only entry fields are `files` and optional
`directory_environment`. Each map key is a relative POSIX destination inside
`current/config/<name>`. Each value requires `source` and `mode`, and accepts
`format` and `bindings` under the same rules as a single-file config. Entry-level
`source`, `mode`, `format`, `path`, and `bindings` cannot be combined with `files`.
Per-file `path` and `directory_environment` are not accepted.

A tree contains 1 to 32 files. Each path has at most 8 components. Absolute paths,
backslashes, empty components, `.` and `..` components, duplicate keys, and files
that are parents of other files are refused. `directory_environment` requires at
least one file with a template `format`. All files share that directory environment;
an argv reference such as `{"config":"settings"}` resolves the tree directory.

Each file retains its mode, rendering, size limit, and permissions. Intermediate
directories for `recreate` files are private and writable by the instance account.
Preparation rejects symlink ancestry. A `seed-once` member uses a framework-created
link to its persistent service-owned seed. Its edits survive starts, as with a
single-file seed. Other files are prepared afresh from the selected release.
Each numbered copy receives its own tree and seeds.

Release review includes every member's declaration and source digest. Rollback
prepares the target release's tree. Backup retains the selected release and private
secret snapshots; recovery renders a fresh tree. Runtime edits and local seed state
remain outside backup coverage. Config trees require client and host deploy-tool
generation 19. Existing single-file declarations retain their previous minimum.

Placeholders have these forms:

| Placeholder | Value |
| --- | --- |
| `{{secret.TOKEN_FILE}}` | Current private value of this role's declared `secret_files.TOKEN_FILE` alias |
| `{{binding.api.address}}` | Resolved endpoint address |
| `{{binding.api.port}}` | Resolved endpoint port, encoded as a string |
| `{{binding.api.url}}` | Resolved endpoint URL, including brackets for IPv6 |
| `{{binding.tool.path}}` | Resolved executable path |

Names come from the role's existing `secret_files` and `bindings` declarations.
There are no expressions, loops, conditions, or arbitrary file reads. Unknown
placeholders, undeclared secrets, mismatched binding kinds, and unresolved bindings
fail preparation before the workload runs. Inserted values are never interpreted
again as placeholders.

Encoding applies to the complete containing value, including literal prefixes and
suffixes:

| Format | Template syntax and encoding |
| --- | --- |
| `json` | Valid JSON with placeholders inside string values, for example `{"password":"{{secret.TOKEN_FILE}}"}`. Keys cannot contain placeholders. Output uses JSON string escaping. |
| `yaml` | Single-line block mappings and sequences with plain identifier keys. Put placeholders in unquoted values. Output uses double-quoted scalars with escaped backslashes, controls, and line separators, preserving other Unicode as UTF-8. The closing quote precedes the line ending, preventing continuation into the next setting. Static quoted scalars and JSON flow values are accepted; multiline scalars, multiline quotes, tags, anchors, aliases, directives, and complex keys are refused. |
| `ini` | Section headers and `key=value` assignments. Placeholders belong only after `=`. Output is a raw value. CR, LF, NUL, other controls, Unicode line separators, surrounding whitespace, backslashes, quotes, `#`, and `;` are refused because portable INI escaping cannot preserve them reliably. A trailing backslash can consume the next assignment as a continuation. `]` and ordinary Unicode remain literal values. |
| `text` | The entire file is one value. Controls and Unicode line separators are refused, including a final newline. No newline is appended, so a trailing backslash has no following line to continue. |

For example, a YAML value can combine `{{binding.api.url}}` with a literal path.
Encoding protects the configuration format; it does not percent-encode URL query
components. For a callback URL containing a shared secret, store the complete URL
as the private value, as in the MediaMTX example below.

Every admitted start reads the protected release template and current private
secret files. The rendered baseline and read-only output are root-owned mode
`0640`, readable by the instance group. `recreate` makes an instance-owned mode
`0600` copy beneath private writable directories, allowing replacement by rename.
The next start discards that copy's edits. A complete view is published atomically;
failed preparation cannot select partial output. Existing invocation retention,
cleanup, and size limits apply. `seed-once` templates are refused because a
persistent seed would retain the old secret after rotation.

Secret intake, rotation, and backups keep their existing owner. Declaring
`secret_files` also supplies the private file path in the corresponding `_FILE`
variable; the value never enters the environment. Rotation restarts the affected
enabled, running consumers, which render again. Framework argv, units, release
files, diagnostics, reports, and public state contain no rendered secret values.
Applications remain responsible for what they write to their own logs.

#### OBS config tree

Declare a top-level secret:

```json
{"secrets":{"obs-password":{"prompt":"OBS WebSocket password"}}}
```

Add these fields to the OBS executable role:

```json
{
	"secret_files": {"WS_PASSWORD_FILE": "obs-password"},
	"configs": {
		"obs-home": {
			"directory_environment": "HOME",
			"files": {
				".config/obs-studio/global.ini": {
					"source": "deploy/files/obs/global.ini",
					"mode": "recreate",
					"format": "ini"
				},
				".config/obs-studio/basic/profiles/stream/basic.ini": {
					"source": "deploy/files/obs/basic.ini",
					"mode": "recreate",
					"format": "ini"
				},
				".config/obs-studio/basic/profiles/stream/streamEncoder.json": {
					"source": "deploy/files/obs/streamEncoder.json",
					"mode": "recreate",
					"format": "json"
				}
			}
		}
	}
}
```

Put this nonsecret template in `deploy/files/obs/global.ini`:

```ini
[Basic]
Profile=stream
ProfileDir=stream

[OBSWebSocket]
ServerPassword={{secret.WS_PASSWORD_FILE}}
```

Put the profile in `deploy/files/obs/basic.ini`:

```ini
[General]
Name=stream

[Output]
Mode=Advanced

[AdvOut]
Encoder=obs_x264
```

Put the encoder settings in `deploy/files/obs/streamEncoder.json`:

```json
{"keyint_sec": 1}
```

Advanced output uses the profile's encoder settings. Simple output's default
keyint of 250 delays HLS segments and RTSP joins. The example sets a one-second
keyframe interval for the x264 encoder.

Supply `FOUNDRY_SERVICE_SECRET__OBS_PASSWORD` through the existing hidden
configuration intake or private `FOUNDRY_SECRET_CONFIG_FILE`. Choose a password
representable by the INI rules above. OBS finds all three writable files beneath
`$HOME/.config/obs-studio/`. Each invocation starts from the release templates
with the current password. To use `XDG_CONFIG_HOME`, set `directory_environment`
to that name and remove `.config/` from all three destination keys.

#### MediaMTX-style YAML

Declare a top-level secret for the complete authentication callback URL:

```json
{"secrets":{"media-auth-url":{"prompt":"Media authentication callback URL"}}}
```

Add these fields to the media executable role:

```json
{
	"secret_files": {"AUTH_CALLBACK_URL_FILE": "media-auth-url"},
	"configs": {
		"settings": {
			"source": "deploy/files/mediamtx.yaml",
			"mode": "rendered-template",
			"format": "yaml",
			"path": "mediamtx.yaml"
		}
	},
	"arguments": [{"config": "settings"}]
}
```

Put this template in `deploy/files/mediamtx.yaml`, matching the configuration keys
of the pinned daemon version:

```yaml
authMethod: http
authHTTPAddress: {{secret.AUTH_CALLBACK_URL_FILE}}
```

Supply `FOUNDRY_SERVICE_SECRET__MEDIA_AUTH_URL` privately. The argv element is only
the prepared configuration path. A daemon that uses a `--config` flag can instead
declare `[{"literal":"--config"},{"config":"settings"}]`. The callback URL is
written as one YAML double-quoted scalar, so quotes, comment markers, YAML
indicators, and Unicode in the private value cannot add YAML keys or lines.

### Release-carried artifacts

Archive build, artifact verification and service bundle activation support this
source. Service views use the verified release inventory digest for release-carried
bundles, with the same platform, ABI, ownership and read-only mount checks as
downloaded bundles.

For an artifact that belongs to the application repository, replace the platform's
`url` and `sha256` fields with a typed `source` object:

```json
{
  "linux-arm64": {
    "source": {
      "release": {
        "path": "deploy/artifacts/linux-arm64/obs-extension.tar.gz"
      }
    }
  }
}
```

The path names one file beneath `deploy/artifacts/` in the frozen application
snapshot. Declare a separate platform entry for each supported architecture.
The builder copies that exact file, computes SHA-256, and records the digest in
`deployment.artifacts` and the release file inventory. It performs no download.
The source declaration stays unchanged, so the bootstrap topology hash remains
stable. The release inventory identifies the acquired bytes.

To produce the file inside the archive builder, add an argument vector to the
`release` object:

```json
{
  "path": "deploy/artifacts/linux-arm64/obs-extension.tar.gz",
  "build": ["sh", "deploy/build-obs-extension.sh"]
}
```

The command runs once for the selected platform from the writable snapshot root,
after the application build and before artifact acquisition. It receives
`ARCHITECTURE`, `GOOS=linux`, `GOARCH`, and the builder's target `CC`. It has a
20-minute deadline. Arguments undergo no shell expansion. Keep the build script
and its inputs in the repository; ignored or excluded files are not build inputs.
The output must not exist before this command runs. A nonzero exit, missing file,
symlink, hard link, duplicate source path, or source change during copying fails
the build. The builder does not search directories or expand globs.

The default `DEPLOY_BUILDER_ARCHITECTURE=native` uses the Docker server's
architecture and cross-compiles Go for the profile's target. An artifact that
links against target-architecture userland, such as apt `libobs-dev`, can select
`DEPLOY_BUILDER_ARCHITECTURE=target` so its command runs in that architecture's
Linux userland. Explicit `amd64` and `arm64` are also accepted. Builder images
and caches are isolated per architecture, and `DOCKER_DEFAULT_PLATFORM` has no
effect on this selection. A non-native builder must pass the pinned Ubuntu
`dpkg --print-architecture` preflight before building.

Emulated target builders are supported for release builds when the Docker engine
can run `linux/<architecture>`, as checked by that preflight. Expect roughly five
times the native build time; see the
[deployment guide](deployment.md#invocation) for the measurement.
Use one when a release-built artifact needs target-architecture packages, such as
an OBS plugin linked against the target's `libobs-dev`. Acceptance and rehearsal
host runs still require native platforms.

```sh
DEPLOY_BUILDER_ARCHITECTURE=target ENVIRONMENT=acceptance-local ACTION=build make release
```

Keep the artifact's existing `format`, exports, modules, and provenance fields.
For example, package an OBS shared library and shader beneath a bundle directory
in a `tar.gz` file, declare the library in `modules`, and export that directory
through `bundles`. The normal Linux architecture checks, bundle ABI evidence,
expansion bounds, and immutable publication still apply.
Change `version` when the bytes change. This source does not permit replacing an
already published artifact version or bypassing the package-service ABI
requirement. A platform cannot combine `source` with `url` or a handwritten
`sha256`.

Build with the application's usual entry point:

```sh
ENVIRONMENT=acceptance-local ACTION=build make release
```

### Numbered copies

Set a custom executable role's count in profile 3 `placement`:

```json
{"placement": {"http": 1, "relay": 3, "worker": 1}}
```

Generation 17 is reserved for v0.36.0 parameterized upload routes. Numbered copies
use generation 18. The tools upgrade plan includes a runtime-v1 bridge when the
installed helper matches the exact v0.35.0 bytes. The upgrade journal records the
source and target digests, and the helper is atomically replaced before selecting
the new bundle. Interrupted upgrades resume forward. Existing singleton invocation
pins and cleanup remain compatible; unknown helper bytes are refused. This bridge
is required because the old fixed helper rejects ordinals above one before
importing its bundled library.

Every tools upgrade also advances an installed network-boot coordinator to the
candidate bundle. Its pin change is recorded in the upgrade journal, atomically
published, reloaded and checked against systemd's loaded command before tools
selection. Resume repeats the reload and verification even if the file already
has the new pin. Application registry writes refuse a stale coordinator or a
capability above its parser generation. This protects new declarations at
generation 14 (templates and public-listener bindings), 15 (Unix bindings),
16 (confinement and shared directories), and subsequent generations, including
17's separately delivered upload-route changes. Tools-upgrade intent writes may
precede the advance only while application state remains unchanged.

Counts above one require generation 18 on both the client and host. The floor
remains selected through rollback and recovery, including a later single-copy
profile. A count of zero means unplaced. Wrappers remain outside release 2a.

Copies have stable ordinals from 1 through the count. `ROLE=relay` selects every
placed relay copy. Each copy gets its own systemd unit, private TCP allocation,
config view, seed files, and persistent state directory. Copy 1 keeps the existing
paths; later copies use `relay@2`, `relay@3`, and so on under the instance's service
runtime and state trees. Unix listeners keep the declared socket name for copy 1
and append `@2`, `@3`, and so on for later copies. Shrinking and growing never
renames surviving copies. Persisted selections, descriptor leaves, reports and
copy displays use the same `role` or `role@N` key. Private snapshot keys append
`.secret` to that key. Socket leaves apply the same suffix to the declared name.
Systemd custom units retain `foundry-app-<instance>-<role>@<ordinal>.service`,
including `@1`, because the ordinal is the template instance. Environment variable
keys retain uppercase underscore components required by the configuration interface.
Existing copy-1 maintenance markers retain `<role>-1.hold`; new copies use
`<role>@N.hold`. These exceptions preserve deployed singleton contracts.
Use `{"copy_number": "self"}` in typed arguments or
environment references for the decimal ordinal; JSON field bindings receive an
integer.

Copies of one role run under the same service account on one host. A program with
its own single-instance check, common in desktop and GUI applications, treats copy
2 as a duplicate of copy 1. Add the program's multi-instance flag to the role's
arguments. For OBS Studio this is `--multi`. Without it, a headless copy waits on
the "already running" dialog before it logs anything or opens its listener, and
readiness fails.

Role-level network confinement and shared-directory grants apply to every copy.
If regrowth restores a deliberately disabled copy, dependents that cannot remain
in the running selection enter the reviewed stop scope. Their exit and network
closure must be proved before the transition can complete.

Readiness verifies each copy's process, listener ownership, stability, and BPF
attachments where confinement is declared. Dependencies include every prerequisite
copy. Units at the same dependency level are queued together without blocking.
Each unit's readiness timeout starts at its own systemd monotonic process-start
timestamp and includes its stability proof. Each selected unit has its own
observer thread and at most one open probe socket, bounded by the 256-unit
instance cap. Network probes never wait for another unit's observer. Only short
local inspections (`systemctl show`, `ss`, and related status commands) share a
16-slot semaphore; their existing timeouts include time waiting for a slot.
Failed inspections release that slot before persisting safe command metadata.
Thread and file journal locks share the remaining inspection deadline, and both
waits honour cancellation. If the deadline expires or cancellation prevents a
write, the caller still receives `command_failed` with a notice that the journal
entry was not recorded. Inspect the failed command privately on the host.
Each unit has its own probe cadence and stability window. A unit that finishes
early remains under observation while slower peers finish; a lost probe or changed process invalidates
its proof. Failure or deadline expiry closes active probe sockets, kills and
reaps owned command groups, and joins every worker before returning. Every worker
outcome is collected. Specific inspection or confinement errors take precedence
over generic readiness failure, in inventory order, including errors raised
during cancellation. Concurrent command failures append to the private journal
under thread and file locks. Consumers start only after their prerequisites pass.
Startup and final readiness still share one overall 90-second deadline.

Build the changed profile into a release archive, then review and activate its
release plan. The archived target profile owns the counts. The plan shows added,
removed, changed, and retained copy identities. Growing allocates and starts the added
copies; copies with unchanged effective specifications keep their process and
allocations. Shrinking stops the highest ordinals first, proves their cgroup,
main PID, control PID, jobs, and children have exited, then removes their unit
files and runtime views. Private reservations remain protected by the existing
network closure and old-reference rules. Persistent state and seed files are
retained, including when a copy is later added again. An uncertain stop leaves
resources retained for reconciliation.

Stop-time cleanup validates the invocation's pinned launch record and unit cgroup
even after the copy leaves the selected profile. Admission, preparation, and running
hooks still require a selected or retained origin. Before removing a proven-stopped
unit, shrinking records any failed state in the private operation journal, resets
only that unit's failure, and verifies it is inactive. Failed resets retain its
unit file and runtime views for reconciliation.

### Profile field reference

Profile 3 retains the existing profile fields, adding `placement` and `listeners`.
`placement` must name every role. HTTP is exactly 1; other built-ins are 0 or 1.
Custom executable roles accept 0–128, with at most 256 concrete units per instance.
All prerequisites of a placed role must also be placed. The builder validates the
profile architecture against artifact platform coverage.

The profile `listeners` map uses `<role>.<listener>` keys for exactly the enabled
public custom listeners. Loopback listeners have no profile entry. Each has:

- `ports`: one explicit port per placed copy, ordered by ordinal, 1–65535 and
  unique across the profile; 22, 80 and 443 are reserved. These are public ports,
  not allocated private ports. See [numbered ports](#one-public-port-per-copy).
- `source_cidrs`: 1–64 unique canonical CIDRs. Each address family must have a
  corresponding public bound address. Choose the actual permitted clients;
  `0.0.0.0/0` is an intentional allow-all IPv4 policy, not a safe default.
- `dns`: `mode: "external"` or `"managed"`, a `hostname`, 1–16 literal `public_addresses`, and
  `proxied: false`. Addresses must be bindable on the host. The profile does not
  provision interfaces or upstream NAT. Managed mode also requires `zone_id` and
  `records`, described below.

`domain` remains the app's HTTP hostname; each raw TCP listener has its own
`dns.hostname`. `edge_origin_lockdown` is the existing HTTP edge policy, not a
replacement for raw TCP source CIDRs. Leave `host_id` null only for initial
bootstrap; retain the committed host binding afterward. Generate and preserve a
separate instance UUID for each environment.

<a id="recipe-bindings"></a>
## Load bindings in the application

Use `github.com/weiloon1234/Foundry-Go/deployment` during assembly, once per process.
The framework unit sets `FOUNDRY_DEPLOY_RUNTIME_FILE` to the role's descriptor.
The schema-2 descriptor contains the unit identity, selected release/environment
and deployment digest, plus schema-1 role bindings. Do not write or discover this
file yourself. It is invocation-specific; private HTTP allocation is selected
with the journaled effective environment, not guessed from the profile.

This consumer fixture loads the three HTTP-role bindings from the worked example:

<!-- compiled-source: tests/fixtures/consumer/hostcapabilities/bindings.go#host-capability-bindings -->
```go
// RelayDependencies contains the relay addresses and tool used by the app.
type RelayDependencies struct {
	// Control addresses the relay's private HTTP control listener.
	Control deployment.Endpoint
	// Public describes the relay's profile-selected public listener.
	Public deployment.PublicListener
	// JSON is the package-owned jq executable for JSON processing.
	JSON deployment.Executable
}

// LoadRelayDependencies runs once during application assembly.
// The caller explicitly chooses its local-development path when present is false.
func LoadRelayDependencies() (dependencies RelayDependencies, present bool, err error) {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return RelayDependencies{}, present, err
	}
	control, err := bindings.Endpoint("relay")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	jsonTool, err := bindings.Executable("json")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	public, err := bindings.PublicListener("relay_public")
	if err != nil {
		return RelayDependencies{}, true, err
	}
	return RelayDependencies{Control: control, Public: public, JSON: jsonTool}, true, nil
}
```

Inject `RelayDependencies` into the component that calls the relay. `Control.URL()`
returns a `url.URL` with the allocated private host/port; append control paths with
the URL API. `JSON.Path` is an absolute verified path, not a PATH lookup;
execute it with an argument vector when the application needs jq for JSON processing.
`Executable` also carries `SHA256`. `Bundle(name)` returns a `BundleDirectory`
with its service-local `Path` and `SHA256`.

`PublicListener(name)` returns the declared role and listener, profile-owned
`Hostname`, public `Port`, ordered `Ports`, and `PublicAddresses`, plus topology-owned `TLS` and
`Transport`. TLS is `deployment.TLSEdge` or `deployment.TLSNone`; transport is
`deployment.TCPTransport`. The application chooses its protocol scheme, such as
`rtsp`, and constructs a URL with `net.JoinHostPort` and `net/url`.
The binding contains no secrets and does not assert current readiness or grant
access. Authentication remains the listener's application responsibility.

The listener must be public and placed. The binding does not add a startup
prerequisite; declare `requires` when startup must wait for its provider.
Public listener records are delivered through the runtime descriptor, not scalar
argv, environment, or rendered-config binding references. Each lookup returns
an owned address slice. The process retains its startup snapshot, and changed
bindings restart consumers through the existing deployment lifecycle.

Role-binding schema 1 adds an optional `public_listeners` collection. The writer
omits it when empty, preserving descriptors and digests for existing bindings.
A v0.32.0 binary can read a new descriptor that has no public listener bindings.
Upgrade the consuming binary before declaring a public listener binding.

`SharedDirectory(name)` returns a disposable shared-file binding described below.

`LoadBindings` returns `present == false` without error only when the environment
selector is absent. An explicitly invalid selector or malformed descriptor is an
error. The caller must choose any local-development alternative explicitly; it
must not turn errors into defaults. Missing named lookups return `fault.Missing`.
`Bindings.Unit()` identifies instance, role and ordinal; `DeploymentSHA256()`
identifies the selected deployment. Keep the immutable snapshot and inject only
the concrete values each component needs. `deployment.Detect()` reads the same
role descriptor. Neither loader downloads artifacts nor replaces host verification.

<a id="recipe-host"></a>
## Host preparation and operations

Use the application's pinned framework tool and the thin Make targets from the
[deployment invocation guide](deployment.md#recipe-deploy). Feature topology
requires generation 13 for release 1 operations and generation 14 for configuration
templates or public listener bindings. Use a Foundry-Go pin containing the required
tool generation. Use the host's existing profile
to review and apply the forward-only tool upgrade. A deployed generation-12 preview
keeps its schema-3 profile for `tools-plan`, `tools-apply` and `upgrade-resume`.
For a legacy host, upgrade before adopting schema 3. Direct upgrades require at
least generation 9; older hosts must reach that generation first. A fresh host gets
the pinned tools during bootstrap.

```sh
ENVIRONMENT=production ACTION=tools-plan make deploy
ENVIRONMENT=production ACTION=tools-apply PLAN_TOKEN='<reviewed-token>' OPERATION='<stable-uuid-v4>' make deploy
```

Keep the operation UUID for a tool-upgrade resume. Review the plan; the apply
command asks for confirmation. Tool generation is independent of the framework's
semantic version: verify the selected bundle reports at least generation 13, or
generation 14 when using templates or public listener bindings, rather than
assuming a version label establishes support.

Before bootstrap, prepare the existing SSH, PostgreSQL/Redis and HTTP certificate
inputs described in the deployment guide. Public TCP additionally needs an active,
dual-family, default-deny UFW configuration on the supported Ubuntu host layout.
Preserve management access and the existing HTTP 80/443 policy. Bootstrap plans
the nginx stream package/module and the declared packages; it does not enable or
reset UFW, switch firewall backends, or remove foreign rules to make a plan pass.
Inspect any ownership or firewall refusal instead of bypassing it.

Do **not** add a persistent `ufw allow 8554/tcp` rule: Foundry owns the listener's
source-filtered runtime opening. It validates live and persistent rules, keeps
persistent feature exposure closed, and opens only after admission and health.
A UFW reload withdraws runtime exposure; use a healthy whole-instance restart to
re-establish it. Boot reconciliation also starts the admitted selection and proves
health before reopening. Unresolved cleanup keeps starts closed.

With the declarations and real artifact values in place:

```sh
ENVIRONMENT=production ACTION=prepare make bootstrap
ENVIRONMENT=production ACTION=deploy RELEASE=build make deploy
ENVIRONMENT=production ACTION=report make deploy
ENVIRONMENT=production ACTION=logs ROLE=relay LINES=200 make deploy
ENVIRONMENT=production ACTION=logs ROLE=relay FOLLOW=yes make deploy
ENVIRONMENT=production ACTION=restart make deploy
ENVIRONMENT=production ACTION=stop make deploy
```

Bootstrap runs `bootstrap-plan`, presents findings and confirmation, then runs
`bootstrap-apply`. It installs declared packages and reserves closed exposure.
Deployment builds/downloads/verifies artifact payloads, prepares the instance,
starts enabled roles in dependency order and opens ingress last. The public port
must be closed before first activation and after stop. Plain restart re-enables every
declared role and placed copy, including after stop, and resumes the selected
instance through admission and readiness in dependency order. Maintenance holds
still prevent held copies from starting; use `up` to lift them. Named restarts
retain their selected scope and refuse held copies. Report lists the listener under its instance with
exposure and DNS observations. Read status after a lost SSH connection; a lost
reply does not establish whether the operation completed.

### Managed Cloudflare DNS

Managed DNS creates A and AAAA records for public listener hostnames. The record's
address determines its family. All records require `proxied: false`, including
plain TCP listeners. Use a separate hostname from a proxied HTTP site.
DNS-only records disclose the origin address and do not provide Cloudflare HTTP
WAF or authentication.

Managed records describe the hostname and addresses, independent of the number
of role copies or their public ports. Count changes and port changes leave DNS
records unchanged. Each numbered stream server inherits the listener's PROXY
setting and uses its own separate non-PROXY readiness listener. Recovery restores
the selected copies before DNS cutover, then uses a fresh public-readiness budget.

Each managed listener declares the following `dns` object:

```json
{
  "mode": "managed",
  "hostname": "relay.example.org",
  "public_addresses": ["203.0.113.10", "2001:db8::10"],
  "proxied": false,
  "zone_id": "0123456789abcdef0123456789abcdef",
  "records": [
    {"ownership_id": "ceadee2d-9789-483a-902c-c7c4ff59cd6a", "address": "203.0.113.10", "ttl": 300},
    {"ownership_id": "944a3de2-287b-47be-a8ae-d0371fda6b52", "address": "2001:db8::10", "ttl": 300}
  ]
}
```

Generate each ownership UUID once and keep it across address and TTL changes.
The records must cover `public_addresses` exactly once. TTL is `1` for automatic
or 60–86400 seconds. An ownership UUID cannot move to another zone, hostname, or
address family. Listeners sharing a hostname must share the same DNS declaration.
Addresses always come from the profile, never SSH or a provider lookup.

The certificate owner supplies the token from its root-only
`/etc/foundry/cloudflare/zones/<zone-id>.ini`. The zone must match the instance's
certificate `zone_ref`. Bootstrap takes `secrets.cloudflare_dns_token` through
private SSH stdin. Feature instances currently retain that token: `ACTION=dns-token-rotate`
refuses until a coordinated managed-record credential rotation exists.
There is no DNS token field in a profile and no service access to this token.

For the shared certificate and managed-DNS token, create these Cloudflare API
token permissions, restricted to **Include / Specific zone / the selected zone**:

- **Zone / DNS / Edit**, named `DNS Write` in the API reference.
- **Zone / Zone / Read**, required by the existing certificate owner's zone lookup.

No account-wide permission or Global API Key is needed. The record endpoints
accept [DNS Write](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/).
The isolated live test below takes an explicit zone ID and needs only DNS Edit on
that test zone. A subdomain test prefix limits test writes; it does not narrow the
token's zone-wide permission.

`release-plan` includes `managed_dns` changes with the prior provider state and
exact desired state. Apply rechecks record IDs, name, family, address, TTL, proxy
mode, ownership comment, provider version, ETag when returned, and a metadata
digest. Any changed state invalidates the plan. Existing unowned records at the
hostname are refused, even when they already have the requested address.

The root-owned instance configuration file `managed-dns.json` records provider
IDs and the full instance, ownership, and creation operation UUIDs. The provider
comment is `foundry:v1:` followed by the 64-character SHA-256 digest of the
canonical JSON array `[instance_id, ownership_id, create_operation_id]`. Its 75
characters fit Cloudflare's 100-character limit on every plan; planning validates
that limit. Verification matches the comment exactly and checks the stored record
ID and hostname. Matching an address or comment alone cannot adopt an existing
record. The encrypted configuration backup includes this file. Take a
new backup after changing DNS if the replacement host must recover those records.

Activation and rollback use the same planner. DNS changes occur after observed
listener closure and private health, before public admission. Removing a listener
deletes only its owned record IDs after closure. Shared records remain while a
listener still declares them. Stopping a service retains its DNS records.
DNS application has a separate 300-second budget. Recovery, activation, and
rollback give public opening a fresh readiness budget after DNS reconciliation.

Each provider mutation has a durable intent before its single write attempt.
Only an HTTP 4xx write response with `success: false` and an entirely allow-listed
set of validation error codes proves rejection. HTTP 408, 409, and 429 are always
uncertain, regardless of their codes. The allow-list is deliberately limited to:

| Code | Fixed diagnostic | Evidence for rejection before mutation |
| --- | --- | --- |
| 9005 | Invalid IPv4 record content. | Cloudflare's [SDK exception example](https://github.com/cloudflare/python-cloudflare/tree/2.19.0#exception-handling) documents this response to creating an A record with invalid IPv4 content. |
| 9313 | DNS record comment exceeds the provider limit. | The owner's test-zone certification observed this validation rejection with no record created; Cloudflare documents the [comment limits](https://developers.cloudflare.com/dns/manage-dns-records/reference/record-attributes/). |

These codes describe invalid record input, not authentication, service availability,
or a conflicting operation. No other code is inferred from its numeric range or
message. Legacy nested error chains, malformed entries, and mixed lists containing
any other code remain uncertain. Cloudflare [removed the generic validation wrapper](https://developers.cloudflare.com/fundamentals/api/reference/deprecations/#2024-10-01);
the deprecated wrapper code 1004 is not allow-listed.

A definite rejection records `rejected`, with later changes marked `not_attempted`,
and returns `dns_provider_rejected`. Earlier applied records retain their ownership
IDs and provider snapshots. The DNS operation closes with `public_admission: "failed"`;
it cannot commit admission or replay writes. Reports include
`managed_dns_provider_rejected` with each record's result and numeric rejection codes.
After cleanup proves workload exit and listener closure, a new reviewed release or
rollback can proceed from fresh provider observations. For example, if A applied
and B was rejected, the next plan binds A's owned ID and B's absence. Restart alone
cannot reopen admission; a reviewed operation must fully succeed.
The corrective plan includes the failed operation's intended running roles,
subject to current enablement, dependencies, and maintenance holds.

Unlisted coded 4xx, HTTP 408, 409, 429, 5xx, incomplete or invalid error responses, timeouts, and lost
acknowledgements remain uncertain and trigger read-back rather than another write.
A lost create acknowledgement is matched to its persisted creation
identity. An absent unknown create remains unresolved and never triggers a second
POST. Updates and deletes reconcile through the same owned provider IDs:

- Exact desired state proves application. Delete is proved by absence of its ID.
- Exact prior state permits a newly reviewed operation, never an automatic retry.
- Any third state requires operator review. Foundry does not delete and recreate
  the record, claim rollback, or open public admission.

The DNS owner retains both states and the operation identity for reconciliation.
Deployment cleanup still owns stopped workloads and admission; DNS read-back
alone cannot clear an unresolved deployment. Reports include record mode, safe
provider snapshots, drift, and unresolved operation IDs. Host readiness, provider
application, and DNS propagation remain separate results.
The DNS operation records `public_admission: "pending"` before lifecycle writes
and changes it to `"committed"` only after successful deployment admission.
If opening fails after provider updates, cleanup closes the workloads and the
report includes `managed_dns_public_admission_pending`, with the operation UUID,
prior and desired records, and fresh provider outcomes. DNS read-back does not
clear this phase or restart the service. The lifecycle owner must reconcile the
deployment before completing admission.

Reports also inspect owned record IDs absent from the selected profile. These
produce `managed_dns_retained_record`, even when the last DNS operation succeeded.
The record reports whether the provider still publishes it, has drifted, is absent,
or could not be observed. A failed listener removal cannot hide retained DNS.

Cloudflare's documented record update endpoint has no conditional-update
contract. Foundry re-reads immediately before writing, but an external editor can
still change a record between that read and the write. Serialize manual provider
edits with deployments. Requests disable redirects and proxies, use a 10-second
socket timeout and a 15-second wall budget, and cap responses at 256 KiB. Record
inventories and operations are limited to 128 records. Diagnostics retain at most
three numeric provider error codes, with fixed framework-authored descriptions for
known validation codes and a generic description for others. Provider free text is
discarded: it never reaches CLI errors, persisted rejection details, or reports.
The complete error-code list is checked before classifying a rejection, even when
only the first three codes are displayed. Errors never print complete API responses.

Replacement-host `RecoveryPublicBinding` uses `dns_cutover: "managed_pending"`
when any listener uses managed DNS. Each managed listener includes
`managed_records`, a list of the typed `managed_dns` change objects. Each object
contains `zone_id`, `ownership_id`, `instance_id`, `create_operation_id`, `prior`,
and `desired`. Use the source report's ownership and provider snapshots for the
IDs and prior state; change only the desired address. The intent preserves the
source binding digest, deployment digest, source and target identities, and
operation UUID. Planning compares custody with the encrypted backup and reads the
provider afresh. Apply compares again after old-server-stopped confirmation and
private takeover health. `managed_applied` proves provider read-back, while
`dns_propagation` reports address observations separately. External listeners in
the same recovery still produce manual instructions. Rehearsal cannot publish a
managed cutover.

The local controlled provider tests exercise the real HTTP adapter and lifecycle
owners without Cloudflare credentials:

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_dns test_dns_live
```

The live test skips unless `FOUNDRY_TEST_CLOUDFLARE_REQUIRED=1`. Supply
`FOUNDRY_TEST_CLOUDFLARE_ZONE_ID`, `FOUNDRY_TEST_CLOUDFLARE_API_TOKEN`, and
`FOUNDRY_TEST_CLOUDFLARE_DOMAIN` privately in the environment. The domain must be
the test zone or a subdomain of it. The test creates random names under
`foundry-dns-test.<domain>` and deletes only IDs with its own creation receipt and
matching random hostname. It never resets the zone. The container feature scenario
also invokes `tests/acceptance_dns.py` against the controlled loopback provider.
Ordinary application rehearsal continues to use external local DNS.

The [Managed DNS live certification](../evidence/v037-managed-dns-live-certification.md)
records the 2026-11-15 test-zone results and reproduction command.
### Select and inspect numbered copies

For a placed numbered role, `ROLE=relay` selects every copy. Add `COPY=2` to
select only its second copy for stop, restart, or logs:

```sh
ENVIRONMENT=production ACTION=stop ROLE=relay COPY=2 make deploy
ENVIRONMENT=production ACTION=restart ROLE=relay COPY=2 make deploy
ENVIRONMENT=production ACTION=logs ROLE=relay COPY=2 LINES=200 make deploy
ENVIRONMENT=production ACTION=logs ROLE=relay COPY=2 FOLLOW=yes make deploy
```

The CLI also accepts `--role relay --copy 2`. `COPY` requires an explicit `ROLE`
and a placed ordinal. Other actions reject `COPY`. Selecting a role without
`COPY` keeps its whole-role meaning. A successful individual operation returns
`status: "degraded"` when the complete required set is not healthy. Stopping a
copy does not stop or disable its siblings. Dependency checks still apply across
all required copies; a partial selection cannot bypass an enabled dependent.

Reports preserve the existing `services[role]` entry for a singleton role. A
multi-copy role has `services[role].copies`, ordered by `ordinal`. Each row carries
its unit state, enablement, admission, maintenance hold, process identity,
readiness, listeners, confinement proof, shared-directory grants, and persistent
state paths. Private listener and confinement rows identify the ordinal for
multi-copy roles. Unavailable observations remain null or unverified, and all
copies share the existing report deadline. Held copies have readiness status
`held`; they do not run readiness probes or count as failed copies.

### Roll back a release with removed copies

Rollback uses the retained target's placement. Surviving copies keep their current
systemd enablement. A removed copy uses the enablement recorded before its removal.
The host retains that evidence for later rollback, including recorded disabled
copies. Missing evidence leaves the copy disabled and the instance degraded.

A rollback plan lists absent target copies in `restored_copies`, with `role`,
`ordinal`, `enabled`, and `evidence` set to `recorded` or `missing`. The plan's
`enabled_roles` and `running_roles` show the effective dependency-checked
selection. Persisted copy selections keep `role` for copy 1 and use `role@N` for
later copies. This encoding also applies to maintenance and recovery records;
it does not change the meaning of the public `ROLE` selector.

### Back up and recover numbered copies

Backup `service_state` coverage has one entry per custom copy. Multi-copy entries
add `ordinal` and `persistent_state_paths`; singleton entries keep their existing
shape. Each copy receives its role's state policy. `local-unprotected` remains
unprotected, and external state still requires its declared recovery owner.

Takeover plans preserve each copy's enablement and holds. Recovery starts copies
in dependency order, after every required prerequisite copy is ready. A held
prerequisite also holds its dependent copies. Rehearsal includes all copies of
its selected safe prerequisite roles and keeps public exposure closed.

Restore drills report each numbered copy's state paths from the selected backup
with `restored: false`. Database PITR does not restore local service files or start
custom services. Disposable caches remain disposable; the drill does not claim
complete instance coverage.

### External DNS and Docker-host NAT

Create external DNS-only A/AAAA records for the listener hostname, pointing at the
intended reachable addresses. Raw TCP is not HTTP: do not enable a provider's HTTP
proxy on this name. `proxied: false` records that requirement; DNS answers alone
cannot prove the provider configuration or a completed global cutover. External mode
does not create, update or delete these application records. Certificate
DNS challenges remain the separate existing certificate owner's responsibility.

Bootstrap and release plans and network reports show the required records and
the recursive lookup state: `resolved`, `unresolved` (resolution failed), or
`unavailable` (the two-second lookup deadline expired or no usable observation
could be collected). A first deployment does not require the hostname to resolve.
External DNS is informational: changed answers do not invalidate a reviewed release plan,
and neither readiness nor public exposure depends on them. Recovery retains failed
observations explicitly and leaves external cutover pending.

Release activation rechecks the reviewed host observations inside its owned
executor. Only that verified executor's newly created systemd unit is excluded;
other activation changes still invalidate the plan. A `stale_plan` observation
error lists changed key names, never values, and records those names in the
private operation directory's `stale-plan.json`. Review a fresh plan after
resolving the reported changes.

Failed executor and cleanup commands have separate private journals in that
operation directory: `operation-commands.json` and
`operation-cleanup-commands.json`. These record command names, systemctl
subcommands, unit names and exit status; they omit other arguments, stdout and
stderr. A failure during initial validation can be retried with a fresh review
only after cleanup proves workload and listener closure and verifies that
publication never began. Uncertain or later failures still require reconciliation.

Both iptables-legacy and the supported iptables-nft/UFW layout are observed. The
network owner inventories `filter`, `nat`, `raw` and `mangle`, including live and
persisted rules. Docker DNS or masquerade rules in NAT `OUTPUT`/`POSTROUTING` can
coexist. Inbound `PREROUTING`/`INPUT` jumps are followed: DNAT or REDIRECT that can
match the claimed TCP port refuses, including broad or ambiguous port guards.
Explicit rules proven disjoint from that port can coexist. Unknown hooks, foreign
allows and raw/mangle behavior that could alter claimed ingress still refuse.
NAT support does not mean arbitrary Docker firewall configurations are accepted.

The addresses in `public_addresses` must belong to the namespace where Foundry
runs. A Docker host's published address is not automatically an address inside a
container. Foundry neither publishes Docker ports nor configures an upstream
router. Do not add Docker port publishing or DNAT over a Foundry-owned port in the
same observed namespace. Test reachability from a peer namespace or another
machine, not from the host loopback; verify permitted traffic reaches the relay
through nginx stream and that stop closes it.

### Maintenance holds and shared application state

While the shared record is confirmed down, HTTP readiness checks accept the
framework's 503 maintenance response as `status: "maintenance"`. Process identity,
listener ownership and stability checks still apply. Continuing custom roles
keep their declared probes; held roles are not probed. An unavailable shared
record or a different HTTP failure does not establish maintenance readiness.
`up` lifts the shared pause before requiring normal HTTP readiness.

Use deploy `down` for immediate fencing of supervised service listeners. Direct
`artisan down` and maintenance changes from another host only update the shared
application record. The next privileged instance action reconciles that record
through the owned lifecycle operation. A host report identifies stopped-policy
services that are still admitting while the shared record is down.

Restore drills, backups, logs, configuration-key inspection, release planning and
stop may reconcile toward down or do nothing; they never restore held workloads
or open their listeners as a reconciliation side effect. After an external
`artisan up`, use deploy `up` or `status` to restore those copies separately.

Each copy has its own root-owned persistent hold marker beneath the instance
control directory. A hold preserves systemd enablement. `up` restores only copies
that remain enabled and declared; stopping one copy during maintenance keeps that
copy disabled after `up`, without changing its siblings.

Held copies are reported separately from failed or unexpectedly inactive services.
HTTP uses its maintenance liveness contract while paused. Deploy, configuration,
restart and boot retain persistent holds; an unavailable shared store never clears
them. `up` removes markers only under pending admission after closure is proven.
If readiness then fails, the operation reports failure and closes the affected
services; it does not claim the application pause was restored. After cleanup
proves closure, an explicit `up` can retry the held selection.

Release and rollback plans expect public exposure only for enabled copies without
active holds. Changing a held role to `maintenance: "continue"` restores it if it
was running before maintenance and remains enabled; the transition must pass its
readiness checks. Failed startup reinstates the hold through independent cleanup.
If the shared store cannot be observed, a declaration change cannot remove an
existing marker. A later successful observation reconciles the policy. A deliberate
disable still prevents restoration.

## Rehearse locally

Use an explicit application profile such as `acceptance-local`. Set
`runtime_environment` to `testing`, use an `acceptance*` environment name and a
`.acceptance.invalid` domain, and assign a non-null `host.host_id` UUID. Preserve
that UUID when building the archive. Set `edge_origin_lockdown` to `false` for the
isolated network. The container adopts this host identity. Rehearsal never contacts
the profile's SSH address or changes the profile or `.deploy/settings.json`.

Run from the application repository with Docker available:

```sh
ENVIRONMENT=acceptance-local go tool foundry deploy rehearse \
  --archive /absolute/path/to/app.tar.gz --ubuntu 24.04
```

Omit `--archive` to use the normal application builder for the selected profile.
The archive must match the project's identity, topology, environment and target
architecture. Feature archives must embed the exact selected profile. A production
archive is refused; rebuild it for the acceptance profile instead of rewriting it.
Use a native amd64 or arm64 Docker engine matching that profile. Ubuntu 24.04 and
26.04 are supported; emulated runs are refused.

For public TCP, give every listener an external DNS-only hostname beneath
`.acceptance.invalid`. Use one host address per family across the profile, within
`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, or `2001:db8::/32`.
The container receives those addresses on a private dual-stack network. Each IPv4
subnet is `/24`; each IPv6 subnet is `/64`. For example, a host at `203.0.113.10`
and a source CIDR of `203.0.113.20/32` lets rehearsal probe both allowed and denied
sources. A source CIDR covering the entire subnet has no denied peer; evidence
marks that check unavailable. It does not count an unavailable check as passed.

Rehearsal creates a local DNS server, synthetic CA and S3 stand-in with fresh
credentials. It runs production `bootstrap-plan`, `bootstrap-apply` and `activate`,
then production readiness and report collection. Separate containers check TCP
closure before activation, permitted TCP/TLS access after activation, source
denials, closure after stop, and access after restart. A final stop precedes
cleanup. TLS probes verify the synthetic CA and listener hostname. UDP sockets
are inventoried without claiming public UDP acceptance.

Feature releases follow the real client's `release-plan` then `activate` flow:
rehearsal reviews when the profile uses schema 3 or host status reports a
`deployment_selection`. The review and activation share a fresh operation UUID
and the returned plan token. Evidence records `activation_operation_id` and the
public plan in the activation phase, including when activation subsequently fails.

Supply required service secrets in the bindings file's existing `configuration`
object, using the exact keys in the plan's `required_service_secrets`. A secret
with `scope: "copy"` requires a separate key for every placed copy; a shared key
does not satisfy those requirements. Rehearsal checks bootstrap requirements too.
Missing or empty values fail with `rehearsal_service_secrets_missing` and the
missing key names. Only requested service keys are sent on a release-plan retry;
application settings stay in bootstrap configuration. Secret values never enter
public plan evidence or terminal output.

When the profile declares `uploads`, rehearsal prepares a dedicated loop-mounted
sparse ext4 filesystem for each upload pool after host preparation and before
`bootstrap-plan`. Each image is sized to that pool's declared hard quota. Images
live in a root-owned `0700` directory outside the application and upload paths;
new mount parents are root-owned `0755` directories. The production mount,
ownership and capacity checks run unchanged. No extra binding or flag is needed.
`evidence.json.storage_fidelity.upload_volumes` records this as reduced fidelity:
it does not certify real block devices or production quota enforcement. Sparse
images also depend on free space in the Docker VM's backing storage.

Package preparation and bootstrap use a separate provisioning network. Before
activation, rehearsal disconnects that network and switches DNS to the local
server, which has no upstream resolver. Runtime has no route to production
endpoints. No Docker host ports are published. The privileged systemd host runs
trusted application code; it is not an untrusted-code sandbox.

For an application with external storage, explicitly bind every cloud disk to
the stand-in. Supply a private file, mode `0600`, with this shape:

```json
{
  "environment": "acceptance-local",
  "configuration": {},
  "storage_disks": ["uploads"]
}
```

`storage_disks` names the application's declared R2 or S3-compatible disks.
Rehearsal supplies isolated test buckets and credentials and preserves the disk's
other settings. R2 maps to conditional create and replace, NFC keys, and no
single-request cap, matching the certified R2 driver. S3-compatible disks retain
their declared `cloud.compatible` block without gaining capabilities.

Multipart conditions require a passing certification record for the exact pinned
stand-in image. The pinned VersityGW v1.10.0 is uncertified: its
`ListMultipartUploads` response omits the requested `EncodingType=url` and fails
S3 listing conformance. Rehearsal of R2 policies above `PartBytes` currently
requires the explicit `allow_single_request_conditions` opt-in below and reports
reduced fidelity. Production R2 behavior is unaffected.

Without the opt-in, rehearsal refuses any bound disk that declares multipart
conditions before creating containers. The error names the disk, the listing
conformance gap, and the exact opt-in to add. A future stand-in upgrade must pass
the certification before the opt-in can be removed. See the
[stand-in certification evidence and command](../evidence/v036-rehearsal-conditional-multipart.md).

To accept reduced fidelity for specific disks, add this optional field to the
private rehearsal binding file:

```json
{
  "environment": "acceptance-local",
  "configuration": {},
  "storage_disks": ["uploads"],
  "allow_single_request_conditions": ["uploads"]
}
```

List only unique names already present in `storage_disks`. This permission applies
only when the pinned stand-in lacks multipart certification. Rehearsal then sets
`single_request_conditions: true` and records those disks and their exact caps in
the deploy-owned runtime descriptor's typed `rehearsal` block.
Attachment policies above `PartBytes` pass startup capability validation.
Application policy maxima and `MaxObjectBytes` remain unchanged. Actual
conditional creates and replacements require a declared size at or below
`PartBytes`; larger or unknown-size writes fail before storage I/O with
`storage.RehearsalLimitExceeded` (`storage_rehearsal_limit_exceeded`). Attachments
retain that typed error and its message naming reduced-fidelity rehearsal.
Unconditional writes retain their normal limits.

The application reads this block through `deployment.LoadRehearsal` from
`FOUNDRY_DEPLOY_RUNTIME_FILE` only when the process carries a nonempty
`FOUNDRY_REHEARSAL_RUN_ID`. Without that identity, assembly never reads the
descriptor, even if its path is empty, missing, unreadable, or malformed. A descriptor containing the block must be a regular,
root-owned file with no group/world write permission; invalid ownership or shape
returns `fault.Invalid`. Its run identity must exactly match the process's
`FOUNDRY_REHEARSAL_RUN_ID`. An absent descriptor, absent block, or mismatched
identity retains normal production capabilities and strict startup limits.
With an identity but no matching grant, one redacted JSON notice per load reports
`foundry_rehearsal_storage_no_grant`; it includes no path, run ID, or file contents.
An unset/empty descriptor path or nonexistent descriptor counts as missing;
other read failures and malformed data refuse while the identity is present.
Without an identity, grant loading imposes no descriptor or schema requirement.
With an identity, valid schema-1 rollback descriptors and schema-2 descriptors
without the block preserve strict capabilities; malformed descriptor shapes refuse.
A present block must belong to a valid schema-2 descriptor; malformed blocks,
including explicit `null`, refuse startup with `fault.Invalid`. Feature consumers
that explicitly call `deployment.LoadBindings` retain its strict binding contract.
The validated descriptor supplies an immutable, per-disk grant to storage and
attachment assembly; a run-ID environment variable alone grants nothing.
Grants apply only to disks configured by the current runtime. Reduced database,
jobs, or maintenance runtimes ignore grants for disks they do not configure.
A configured granted disk must still use `s3_compatible`, with a cap equal to
`PartBytes`, single-request conditions, and conditional create or replace support.
The deploy tool validates granted disk names against the application's declared
disks before rehearsal.

No application configuration key enables this mode. The retired
`cloud.conditional_write_mode` key is rejected, even with a run identity.
The deploy tool generates the block only from its private disposable-host context
after verifying the matching acceptance profile. Production profiles, archives,
and generic configuration changes cannot issue it. Rehearsal application units
and console commands receive the matching identity and descriptor.

The application emits one redacted JSON startup notice per affected disk per
process. It lists the runtime cap, `MaxObjectBytes`, and declared attachment
`MaxStoredBytes` values above the cap. Refusals emit disk-only JSON counters;
`Disk.RehearsalRefusals()` and the optional
`foundry_storage_rehearsal_refusals_total` metric expose the process count.
No object keys, upload names, contents, or credentials appear in these records.
Rehearsal collects the public fields into
`evidence.json.storage_fidelity.reduced_disks` and each observation's report.
`runtime_refusals_observed` counts refusal events in the bounded journal window
(last 4096 records per unit); `policy_observation: "pending"` means the startup
notice has not been observed. Process counters reset on restart.

This opt-in does not certify large conditional writes. Once the image is
certified, the same binding preserves multipart conditions without a cap.
Production R2 capabilities and strict S3 listing checks are unchanged.

`configuration` supplies literal application configuration values
through the production bootstrap interface. Use rehearsal-only values; host file
references are refused. Missing cloud-disk bindings stop before container creation.
Other external dependencies have no automatic stand-in: applications requiring
them fail configuration or readiness on the isolated network.

```sh
chmod 600 /absolute/private/rehearsal-bindings.json
ENVIRONMENT=acceptance-local go tool foundry deploy rehearse \
  --archive /absolute/path/to/app.tar.gz --ubuntu 26.04 \
  --bindings /absolute/private/rehearsal-bindings.json \
  --output /absolute/path/to/new-evidence-directory --cpus 4 --memory-gib 8
```

The output directory must be new. Defaults are two CPUs and 4 GiB of memory;
supported limits are 1–32 CPUs and 2–64 GiB. `evidence.json` records archive, tool,
image, topology and profile digests, the package plan, resource limits, allocated
ports and unit observations in reports, probe results, and phase outcomes.
`resources.json` records the exact Docker names and run ID. Only resources bearing
that run's ownership label are removed. Successful cleanup removes private files
and keeps evidence. Cleanup failures preserve private files and fail the run.
`--keep-failed` retains a failed run explicitly and prints its identity and evidence
directory for inspection; it does not retain successful runs.

Upload stand-in mounts are unmounted and their images removed before container
cleanup, including failed or interrupted runs. A partial provisioning failure
rolls back its attempted mounts. `--keep-failed` keeps completed mounts and images
with the failed host for debugging and reports that retention. If unmounting
fails, rehearsal fails and retains the containers and private files so cleanup
can be retried. For a retained host, release upload volumes before removing it:

```sh
docker exec -e FOUNDRY_DISPOSABLE_HOST=1 -e FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1 \
  -e FOUNDRY_REHEARSAL_RUN_ID="$RUN_ID" "$HOST_CONTAINER" \
  python3 -B /verification/deploy/local/rehearsal_host.py release-upload-volumes
```

Use the run ID and host container name from `resources.json`. This cleanup stops
the application's services and nginx before unmounting, and can be retried.

Synthetic tests do not establish real DNS-01, provider policy, global DNS cutover,
cloud firewall behavior, sustained capacity or business recovery. Production
refusals remain refusals. Release-carried bundles use the same verified service
views as HTTPS artifacts.
The framework acceptance runner uses the same shipped host images, CA setup and
systemd container helpers; its fixture-specific scenarios and private module proxy
remain separate from application rehearsal.

The [deploy acceptance fixture](../../tests/fixtures/deploy-acceptance/README.md)
has an `acceptance-run` overlay that covers upload mounts, release planning
and two custom-role copies in one run. Its builder prepares an isolated source
copy and generates private per-copy bindings; the ordinary acceptance topology
and phases remain unchanged. Follow its build and rehearsal commands to exercise
the current checkout on a native arm64 Docker engine.

## Loopback-confined roles and wildcard private listeners

Some daemons accept a control port without accepting a listen address; obs-websocket
is one example. Declare `"network": {"confinement": "loopback"}` on the role and
`"wildcard": true` on each such private TCP listener. Upgrade client and host tools
to generation 16 first. Activation, rollback and recovery retain that instance floor,
even when the selected release no longer declares confinement.

The following complete role is a small worked example of the same binding behavior.
Add it alongside the required built-in `http` role in topology schema 2, and add
`"control": 1` to profile schema 3's `placement`. This example uses Ubuntu 24.04's Python 3.12 packages; on another host, declare
the versioned package that owns the resolved executable. It needs no public listener binding:

```json
{
  "name": "control",
  "kind": "executable",
  "packages": ["python3.12", "python3.12-minimal"],
  "executable": {"package": "python3.12-minimal", "path": "/usr/bin/python3.12"},
  "network": {"confinement": "loopback"},
  "arguments": [
    {"literal": "-I"}, {"literal": "-m"}, {"literal": "http.server"},
    {"listener_port": "api"}, {"literal": "--bind"}, {"literal": "0.0.0.0"}
  ],
  "listeners": {"api": {"transport": "tcp", "bind": "loopback", "wildcard": true}},
  "readiness": {"kind": "tcp", "listener": "api", "timeout_seconds": 15},
  "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
  "limits": {"memory_bytes": 67108864, "cpu_percent": 100, "tasks": 16},
  "state": {"kind": "disposable"}
}
```

A consumer declares `requires: ["control"]` and an endpoint binding with role
`control`, listener `api`, and scheme `http`. Its descriptor contains the allocated
loopback address and port. For an OBS role, apply the same network/listener fields,
use the pinned OBS executable and its service-local configuration, and populate
the websocket server port setting through a typed `rendered-json` field binding
to `{"listener_port": "api"}`. Keep daemon
authentication enabled and pass its password through a declared secret file/template.
A TCP readiness probe checks the listener; it does not authenticate a websocket session.

Foundry renders `IPAddressDeny=any` and `IPAddressAllow=localhost` into the role's
systemd unit. Every admitted start and readiness check verifies the effective IPv4
and IPv6 policy and attached ingress/egress BPF programs on the live cgroup. Missing
cgroup-v2/BPF support, an unobservable attachment or a policy override refuses the
start or activation. The same verifier owns recovery rehearsal isolation.

Confinement applies to the whole role, including outbound IP traffic: it can talk
to loopback services and Unix sockets, but cannot connect to remote databases,
remote APIs or external stream destinations. A role that streams off-host cannot
use this confinement policy. Public listeners on confined roles and confinement of
the public built-in HTTP role are rejected. A wildcard declaration without
confinement is rejected even in recovery rehearsal.

Readiness accepts `0.0.0.0`, `[::]` or `*` only on the allocated port, retaining
process identity, ownership and stability checks. The port remains reserved across
all host addresses and both IP families, including stopped historical allocations.
A new allocation cannot collide with another instance, a public edge listener or
Foundry's shared services. Such listeners create no firewall openings or nginx
routes. Reports list them under `private_listeners` with their wildcard and
confinement declarations, and show the live confinement proof separately. Failed
proof or foreign ownership cannot hide a wildcard socket from exposure findings.

## Shared file directories

Topology 2 can grant roles access to named disposable caches. Both the client and
host require deploy-tool generation 16. The instance keeps that minimum generation
after rollback or recovery. A shared directory name also becomes its binding name
for every listed role, so it cannot collide with another binding in those roles.

Each directory has exactly four declaration fields: `state`, `max_bytes`,
`writers`, and `readers`. Only `"disposable"` state is supported. At least one
writer is required. Readers can be empty. Each role must exist in the topology
and appear at most once across both lists. A topology can declare at most 32
shared directories. `max_bytes` must be a multiple of 65,536, from 65,536 through
1,099,511,627,776 bytes. The alignment prevents rounding above the requested limit
on supported hosts with 4 KiB, 16 KiB, or 64 KiB pages.

The host path is `/run/foundry/services/<instance UUID>/shared/<name>`. Each
directory is a separate tmpfs mount owned by the instance account with mode
`0700` and `nodev,nosuid,noexec`. The host-side `shared` parent is root-owned with
mode `0700`; its ancestors remain root-owned. `/run` matches
the existing per-instance socket and invocation ownership tree and gives the
cache an explicit reboot lifetime.

### Scheduler-to-OBS example

An application's scheduler prepares media. An application-owned OBS wrapper reads
the resulting files and configures OBS media sources with those paths. Add this
field beside `roles` in `deploy/topology.json`:

```json
{
	"shared_directories": {
		"render-cache": {
			"state": "disposable",
			"max_bytes": 67108864,
			"writers": ["scheduler"],
			"readers": ["obs"]
		}
	}
}
```

Declare `scheduler` as a builtin role and place both `scheduler` and `obs` in the
profile. Keep the OBS wrapper's existing executable, listener, readiness, and
resource declarations. Add this environment entry to its executable role:

```json
{
	"environment": {
		"RENDER_CACHE": {"shared_directory_path": "render-cache"}
	}
}
```

The wrapper reads `RENDER_CACHE` and opens `prepared.mp4` beneath it. If the wrapper
takes the directory as a positional argument, add
`{"shared_directory_path": "render-cache"}` to its `arguments` array instead.
These are literal paths, with no shell expansion. OBS does not interpret the
Foundry reference itself. The wrapper supplies the resolved file path to OBS.

One application binary runs HTTP, worker, scheduler, and console processes.
Load bindings once during assembly and register the media preparation handler
only when this process holds the grant. For example:

```go
func mediaPreparationCache() (deployment.SharedDirectory, bool, error) {
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return deployment.SharedDirectory{}, false, err
	}
	cache, err := bindings.SharedDirectory("render-cache")
	if errors.Is(err, fault.Missing) {
		// HTTP, worker, and console processes have no grant; assembly continues.
		return deployment.SharedDirectory{}, false, nil
	}
	if err != nil {
		return deployment.SharedDirectory{}, false, err
	}
	if cache.Access != deployment.DirectoryReadWrite {
		return deployment.SharedDirectory{}, false, fmt.Errorf("media preparation requires write access")
	}
	return cache, true, nil
}
```

When the returned boolean is true, inject the `deployment.SharedDirectory` into
the media preparation handler. Otherwise, continue assembling the application
without that handler. Write completed media to a temporary file in `cache.Path`,
close it, then rename it to `prepared.mp4` so readers never see a partial replacement.

`cache.Path` is the shared path. `cache.Access` is `DirectoryReadWrite` for the
scheduler and `DirectoryReadOnly` for an OBS wrapper that uses Go bindings.
`cache.MaxBytes` is the declared limit. Lookups for missing or ungranted names
return `fault.Missing`. The descriptor omits `shared_directories` for roles with
no grants. Older strict readers need an application upgrade before receiving a grant.
Console calls that the deploy tool makes never receive shared directories. These
are release checks and the maintenance, recovery and rehearsal commands. Their
descriptor keeps the HTTP descriptor's other bindings and runtime fields but omits
`shared_directories`, so the same lookup returns `fault.Missing`. The HTTP unit's
own descriptor keeps its declared grants. Commands typed by hand in `ACTION=shell`
run outside the deploy tool and receive no deployment bindings at all:
`deployment.LoadBindings` reports `present=false`, as in earlier releases.

The directory grant does not imply startup order or file readiness. Add
`"requires": ["scheduler"]` to the reader when it must start after the scheduler.
The reader still needs to handle an empty cache until the first scheduled job
publishes media. Files must be reproducible after every cache reset.

The compiled [fixture consumer](../../tests/fixtures/deploy-acceptance/internal/app/feature.go)
uses the same API with a builtin HTTP writer and a custom reader. Its container
scenario checks actual reader writes inside the service sandbox, including an
overwrite attempt, and checks that an unlisted role cannot read the cache.

### Enforced limits and access

Linux tmpfs enforces `max_bytes` against allocated file-data blocks. A write that
exhausts the filesystem fails with `ENOSPC`. Foundry also limits the inode count
to `min(65536, max_bytes / 4096)`. Empty-file floods therefore have a finite limit.
The [kernel tmpfs documentation](https://docs.kernel.org/filesystems/tmpfs.html)
describes these filesystem limits.

The byte limit does not reserve RAM, limit sparse files' apparent lengths, or cap
all filesystem metadata memory. Tmpfs consumes RAM and may use swap. Host or
service memory pressure can fail work before the filesystem fills. Choose a cache
size that fits the host and the writers' memory budgets. Applications remove
obsolete files and handle `ENOSPC`. There is no timer that races applications to
delete files, and no fallback to an unbounded ordinary directory.

All roles share the instance account. Mode bits cannot distinguish a reader from
a writer. The host parent `/run/foundry/services/<instance UUID>/shared` is a
root-owned directory with mode `0700`, verified without following symlinks.
Instance-account processes outside units cannot traverse it, including readiness
probes and operator invocations. Traversing such a process's `/proc/<pid>/root`
still encounters this root-only parent and fails with `EACCES`.

Each workload gets a private empty `TemporaryFileSystem` view of the shared parent.
Foundry binds only granted directories into that view and sets `ReadWritePaths` for writers
or `ReadOnlyPaths` for readers. Readers also get a read-only bind mount. This
policy applies to builtin app roles and custom roles under `ProtectSystem=strict`.
Unlisted roles receive no directory binding or filesystem view of the cache.
Systemd resolves bind sources against the host root and sets up these mounts as
root before changing to the unit's `User=`. The private parent view remains
searchable by the instance account; its mode is independent of the host parent.
See systemd's [bind mount setup](https://github.com/systemd/systemd/blob/v259/src/core/namespace.c)
and [namespace and credential ordering](https://github.com/systemd/systemd/blob/v259/src/core/exec-invoke.c).

Every role with a shared-directory grant also runs with `PrivateUsers=yes`,
including builtin writers and custom readers. Systemd creates a separate,
root-owned user namespace for each workload and maps root and the instance's UID
and GID to themselves. File ownership and Unix socket peer credentials therefore
retain their existing values. Roles without grants keep their existing policy.

Linux gates `/proc/<pid>/root`, `cwd`, and `fd` traversal with
`PTRACE_MODE_READ_FSCREDS`. Across user namespaces, the capability check requires
`CAP_SYS_PTRACE` in the target namespace. Matching the instance UID is insufficient.
Sibling roles and unprivileged instance-account processes outside the units cannot
use those procfs links to enter a granted role's filesystem view. All workload
units retain an empty `CapabilityBoundingSet`. A role cannot unmount its read-only
view to expose the host's writable mount; inherited mounts are also locked when
copied into a less privileged mount namespace. See the
[kernel capability checks](https://github.com/torvalds/linux/blob/v6.18/security/commoncap.c),
[procfs link checks](https://github.com/torvalds/linux/blob/v6.18/fs/proc/base.c), and
[systemd user namespace policy](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#PrivateUsers=).

Roles still share one instance account for everything outside shared directories.
This policy does not give each role separate account-owned state or IPC authority.
The privileged `+` per-start helper runs as root but still sees the unit's
filesystem namespace, including its shared-directory mounts. It verifies that
view; it does not create or mount caches.

### Lifetime, reports, and cleanup

Deploy operations prepare caches in the host mount namespace before loading or
starting instance units. This covers activation, release transitions, rollback,
recovery and rehearsal, maintenance up, and explicit start or restart. Preparation
holds the instance's shared-directories lock and verifies that its mount namespace
matches PID 1's. At reboot, the root admission boot coordinator prepares caches
after network closure and before the instance units ordered after it start.
The later boot opener checks readiness before reopening traffic.

Host preparation checks the mount source, filesystem type, capacity, inode limit,
owner, mode, and absence of symlinks. Existing files beneath an unmounted directory
and foreign mounts cause refusal. Systemd creates a missing `TemporaryFileSystem=` mount
point with mode `0755` at unit start, before the privileged hooks run. Preparation
and discard accept a host parent only when it is a real, nonsymlink directory
owned by root:root with mode `0700` or exactly `0755`. They tighten `0755` to
`0700` with `fchmod` on the opened directory and re-verify it before mounting or
unmounting any cache. Other owners, types, and modes are refused unchanged.

Each admitted start uses its privileged hook only to verify granted views: each
must be an exact tmpfs mount with its declared source, size and inode limits,
instance owner, and mode. Readers must see a read-only mount. Ungranted roles
verify no cache views. The `+` prefix does not move this hook into the host mount
namespace. If a granted bind source is missing, systemd can fail with
`226/NAMESPACE` before any hook runs. Run the deploy action again to prepare host
paths, or use reboot recovery; no unit hook can repair a missing bind source.

Ordinary restarts preserve the cache. Reboot clears it. A release change or shared
directory declaration change discards the previous caches after every old role
has stopped. Rollback can therefore start with an empty cache. Cleanup uses a
normal unmount and removes only the empty underlying directory. A busy or foreign
mount fails cleanup and remains available for inspection. It never uses forced
or lazy unmounts, and never recursively deletes an occupied directory.

Reports include `shared_directories` with `state: "disposable"`, `backed_up: false`,
`restored: false`, the configured limit, used bytes, and the observed mount status.
Reports never change permissions. A root:root `0755` parent with no entries and
no mounts reports `status: "not_prepared"`, without a finding or used-byte count.
A `0755` parent with content or a mount, or any other unverifiable state, produces
`shared_directory_unverified`. Backup coverage
records the same exclusion. Backups retain declarations but never cache files;
recovery prepares fresh caches. Run reports as root to verify the mounts and
measure used bytes. Unprivileged reports retain the disposable coverage but mark
the inaccessible mounts unverified and omit used bytes.

The deploy tool has no instance-removal action. During manual instance removal,
stop and disable every role first and verify that all unit processes have exited.
Inspect each declared mount with `findmnt --mountpoint <path>` and verify its
`foundry-shared-<instance UUID>-<name>` source and tmpfs type. Unmount that exact
path without force, then remove its empty underlying directory with `rmdir`.
Retain any busy, foreign, or nonempty path for investigation. Never remove the
shared parent recursively while mounts exist. A later host reboot also removes
these runtime-only caches.

## Numbered copy consumers

The release 2a consumer contract supports distinct secrets and endpoints for each
placed executable copy. Ordinals are dense from 1 through the role's count.
Builtins remain singletons. Generation 18 supports counts up to 128 per custom
role, with at most 256 placed units per instance.

### Copy-scoped secrets

Declare scope at the topology's existing secret owner:

```json
{
	"secrets": {
		"worker-password": {"scope": "copy", "prompt": "Worker control password"},
		"license": {"prompt": "Shared worker license"}
	}
}
```

The executable role references those names through its file interface:

```json
{
	"secret_files": {
		"PASSWORD_FILE": {"secret": "worker-password", "scope": "copy"},
		"LICENSE_FILE": "license"
	},
	"configs": {
		"settings": {
			"source": "deploy/files/worker.ini",
			"mode": "recreate",
			"format": "ini"
		}
	}
}
```

The short reference `"PASSWORD_FILE": "worker-password"` inherits the declaration's
scope. An explicit reference scope must match that declaration. Omitting scope
preserves the existing shared secret behavior and configuration key names.

For a role named `renderer`, the configuration owner derives
`FOUNDRY_SERVICE_SECRET__COPY__RENDERER__1__WORKER_PASSWORD` for copy 1 and the
corresponding `__2__` key for copy 2. Names are uppercase with hyphens replaced by
underscores. Collisions are rejected. Use the returned requirement keys with the
existing hidden prompts or `FOUNDRY_SECRET_CONFIG_FILE` configuration input. The
`_FILE` input alias remains available; never supply both forms for one key.

Bootstrap and release-plan review request only missing values for placed copies.
Growth from two copies to three requests only copy 3's password. Shrinking to one
keeps copies 2 and 3's private snapshots. Regrowth and rollback reuse those values.
Shrinking does not erase credentials. The existing encrypted configuration backup
includes retained snapshots.

Each unit receives its own `PASSWORD_FILE` path. Its explicit `secret_file`
arguments and template references use the same selected path. For example,
`deploy/files/worker.ini` can contain:

```ini
[control]
password={{secret.PASSWORD_FILE}}
```

`recreate` renders the copy's current value on each start. Private snapshots keep
root ownership, instance-group access, mode 0640, and no-follow reads.
Copy-scoped secrets give each copy its own value. Like role secrets, they are
readable by every role of the instance because roles share one account. Rendered
baselines and recreated configs do not isolate secret values between roles either.
Secret bytes stay out of argv, unit files, descriptors, public plans, and framework
diagnostics. Applications remain responsible for their own logging.

### Lists of placed copies

A scheduler can consume every renderer's control endpoint and password file:

```json
{
	"name": "scheduler",
	"kind": "builtin",
	"requires": ["renderer"],
	"bindings": {
		"renderers": {
			"copies": {
				"role": "renderer",
				"listener": "control",
				"scheme": "http",
				"secret": "worker-password"
			}
		}
	}
}
```

The target must be an executable role with the named listener. `scheme` is `http`
or `tcp` for TCP listeners, and `unix` for Unix listeners. An optional `secret`
must name a copy-scoped secret used by that provider's `secret_files`. Copy binding
edges and explicit `requires` edges must not form a cycle. Declare `requires` when
startup must wait for the provider; Unix consumers require their provider.

`Bindings.Copies(name)` returns `[]deployment.Copy` ordered by `Ordinal`:

```go
copies, err := bindings.Copies("renderers")
if err != nil {
	return err
}
for _, copy := range copies {
	// Pass these concrete values to the application-owned worker client.
	client := NewWorkerClient(copy.Ordinal, copy.Endpoint, copy.SecretFile)
	workers = append(workers, client)
}
```

Each `Copy` contains `Ordinal int`, `Endpoint deployment.Endpoint`, and
`SecretFile string`. TCP endpoints have an address and private port. Unix endpoints
have `Directory`, `Name`, and `Path`, including the copy suffix. `SecretFile` is
empty when no secret path is included. The files use the instance's ordinary
secret-file permissions; a binding supplies paths, not additional isolation.
Each lookup returns an owned slice of values, so caller mutations cannot change
the startup snapshot. Unknown or ungranted names return
`fault.Missing`. Shrinking removes those ordinals from the list. A declared list
can be empty when its provider is unplaced and no prerequisite requires it.

The descriptor adds `bindings.copies` only for consumers that declare a list.
Each entry has `name` and `copies`; each item has `ordinal`, `endpoint`, `scheme`,
and an optional `secret_file`. Empty outer collections are omitted. Existing
roles without list bindings retain their descriptor shape and digests. Upgrade a
consuming binary before declaring a list, because older strict readers reject
new fields. Lists are available through the descriptor, not scalar arguments or
template binding placeholders.

Console calls retain the HTTP descriptor's copy endpoints, as they retain other
HTTP bindings. They omit every `secret_file`, along with `shared_directories`.
This keeps secret paths out of console descriptors while preserving the v0.35.0
HTTP selection rule. The omission limits descriptor contents; it does not restrict
file access for processes using the shared instance account.

### One public port per copy

For two placed `renderer` copies with a topology-declared public TCP listener
named `ingress`, the profile can bind:

```json
{
	"listeners": {
		"renderer.ingress": {
			"ports": [9551, 9552],
			"source_cidrs": ["203.0.113.0/24"],
			"dns": {
				"mode": "external",
				"hostname": "render.example.org",
				"public_addresses": ["203.0.113.10"],
				"proxied": false
			}
		}
	}
}
```

List position selects the ordinal: port 9551 routes to copy 1's private backend,
and port 9552 routes to copy 2's. The list length must equal the placed count.
Ports are unique across the profile, with the existing reserved-port rules.
Each copy gets its own nginx stream server and firewall opening for the selected
addresses and permitted source CIDRs. No shared-port load balancing is inferred.
The topology still owns TLS and authentication policy.

A `public_listener` binding exposes `PublicListener.Ports []deployment.PublicPort`.
Each entry contains `Ordinal int` and `Port uint16`. The slice is ordered and
caller-owned. `PublicListener.Port` remains the first copy's port for existing
singleton callers. Singleton wire records stay unchanged; numbered records add
`ports: [{"ordinal": 1, "port": 9551}, {"ordinal": 2, "port": 9552}]`.
The Go reader also supplies a one-entry `Ports` slice for legacy singleton records.
