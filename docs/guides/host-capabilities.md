# Host capabilities preview

The B24 preview deploys an application and single-instance custom services on a
generation-12 Foundry host. It installs declared distribution packages, verifies
pinned artifacts, prepares service-local configuration and bundle views, and
starts roles through admission in readiness order. Applications receive typed
bindings to private endpoints and executables. Public TCP uses nginx stream and
source-filtered UFW rules; opening follows whole-set health verification. Restart,
boot admission, reports, logs and whole-instance stop are integrated.

This guide describes topology schema 2 and profile schema 3. Existing topology-1
applications and generation 9–11 hosts keep their existing behavior. Upgrade tools
before enabling these declarations. The app-facing declarations and binding API
are intended to remain stable as later tool-side lifecycle support arrives.
See the [deployment guide](deployment.md) for SSH setup, credentials, project
identity, HTTP certificates and the ordinary application release layout.

<a id="agent-entry"></a>
## Agent entry

- [Check preview limitations](#recipe-preview)
- [Declare the app and relay](#recipe-declarations)
- [Load application bindings](#recipe-bindings)
- [Prepare and operate the host](#recipe-host)

<a id="recipe-preview"></a>
## Preview boundary

Feature execution fails explicitly when an unsupported operation is requested.
It does not fall back to the legacy lifecycle. The following lifecycle refusals
use `capability_unavailable` and name **the post-preview B24 lifecycle release**;
that is a capability milestone, not a promised framework version or date.

| Refused in the preview | Consequence |
| --- | --- |
| Encryption key rotation | The dedicated `keys rotate` action remains gated for feature instances. Generic edits cannot change the keyring contract. |
| Changed reprovisioning of a selected instance | Changes to its profile, package requirements or owned network/boot files refuse. Unchanged re-bootstrap is supported. |
| Newly required packages at activation | Bootstrap installs packages; deployment cannot silently add missing packages. |
| Named lifecycle operations | Start/restart/stop the complete enabled instance. Selecting a role for logs is supported. |
| Interrupted-operation reconciliation and recovery | Do not assume an interrupted operation rolled back. Inspect status and retained cleanup evidence. Unresolved cleanup blocks later starts. |
| Bundle ABI without package-version evidence | Bundle consumers must use package executables with one matching observed service package version. An artifact executable cannot supply this preview proof. |
| Feature release/artifact pruning and helper-bundle pruning | Explicit feature pruning refuses. Automatic helper retention keeps every helper bundle and upgrade journal with a capability diagnostic. Invocation/view limits still apply; reaching a limit refuses further starts. |
| Host capacity changes involving features; backup configuration/run, restore drill and recovery operations for feature instances | These owners are not integrated with feature lifecycle yet. Legacy neighbors retain their existing supported operations. |

The server feature-operation allowlist is `bootstrap-plan`, `bootstrap-apply`,
`release-plan`, `activate`, `rollback`, `restart`, `stop`, `status`, `logs`, `sync`, `config-keys`,
`config-set`, `down`, `up` and `report`.
Host tool upgrades have their own upgrade protocol. Other feature mutations are
not implied by a command appearing in the legacy deployment guide.

Service secrets use the existing private configuration intake. Declare names in
`secrets` and map service `_FILE` variables through `secret_files`; explicit
`secret_file` arguments and per-start configuration references also receive paths.
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
coordinator verifies the complete enabled set before opening traffic. A declared
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
| Wrappers, role instance counts above one, ordinals other than one | B24 release 2 |
| PROXY protocol | B24 release 2 |
| Managed application DNS records | B24 release 2; preview DNS is external only |
| UDP listeners | Later network capability; the parser does not promise a numbered release |
| Artifact formats other than `file` and `tar.gz` | Unsupported format; no later numbered release is promised |

These declaration failures use schema/capability validation errors rather than
silently dropping fields. In particular, plain TCP **is** supported: declare
`tls: "none"` explicitly and acknowledge application authentication.

<a id="recipe-declarations"></a>
## Declaration ownership

| File | Owns |
| --- | --- |
| `deploy/project.json` | Schema-1 project UUID and slug; retain this identity across environments. |
| `deploy/topology.json` | Schema-2 role names, commands, packages, artifact pins, private listener intent, readiness, dependencies and binding names. |
| `deploy/environments/production.json` | Schema-3 host/instance identity, placement, public ports, source CIDRs, external DNS intent and existing environment settings. |
| `deploy/files/` | Nonsecret configuration templates referenced by topology. |
| Application source | Binding consumption, HTTP authorization callback and business authorization. |
| Operator's DNS provider | External DNS records; the preview reports intent but does not create application records. |

The application release builder includes the topology and verified artifact
payloads. Operators supply placement and exposure policy in the profile. Do not
put allocated private addresses, generated role descriptors or host filesystem
paths into either declaration.

### Worked example: app with an authenticated media relay

This example defines an HTTP app plus a generic relay. Supply a Linux relay binary
that implements the argument interface below: a loopback HTTP control server with
`GET /ready`, a plain TCP media server, and an HTTP authorization callback client.
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
        "ingress": {"transport": "tcp", "bind": "public", "tls": "none", "authentication": true}
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
Edge TLS still requires service authentication. PROXY protocol is unavailable, so
do not depend on the relay seeing the original client IP through the proxy.

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
- **Artifacts:** `version` is a single path component. `platforms` maps
  `linux-amd64` and/or `linux-arm64` to an HTTPS `url` and 64-hex `sha256` of the
  download. URLs cannot carry credentials, query strings or fragments. Declare
  the selected profile platform for every artifact. An artifact version cannot
  be reused with changed bytes. `executables` maps export names to relative
  members. `file` exports exactly one executable; `tar.gz` can also export
  `bundles`, a map of names to relative directories. Optional `modules` names
  members beneath those directories. Bundles require `provenance` with
  `source_revision`, `build_inputs`, `license` and `service_abi` strings; the
  preview requires `service_abi` to match the observed consuming package version.
- **Bundle views:** a role's `artifact_mounts` maps local names to
  `{"artifact":"assets","bundle":"templates","area":"config","path":"templates"}`
  (or area `data`). A binding `{"bundle":"<mount-name>"}` or argv
  `{"bundle":"<mount-name>"}` selects its protected service-local view. These are
  not arbitrary host mounts. The preview ABI restriction above applies.
- **Argv:** `arguments` is a list of single-tag objects, each one argument; it is
  not a shell command. `literal` does not interpolate variables. `listener_address`
  and `listener_port` refer to that role's listener. `binding` names a declared
  binding and supplies an executable path, bundle path or endpoint URL. `config`
  names a declared configuration. `state_path` and `runtime_path` contain relative
  paths; `config_root`, `data_root` and `copy_number` use the value `self` (the
  only copy is 1). `secret_file` supplies a private file path, never secret bytes.
  Maximum 128 arguments, 32 KiB combined values, 4096 bytes per literal.
- **Listeners:** only TCP; `bind` is `loopback` or `public`. Loopback listeners
  omit `tls` and `authentication`. Public listeners require `tls: "edge"` or
  `"none"` and `authentication: true`. A public declaration still uses a private
  service backend. Its public address is owned by the profile and nginx.
- **Readiness:** one local `tcp` or `http` probe naming a declared listener.
  `timeout_seconds` is 1–300 (default 30). HTTP requires a local `path` without
  query/fragment and a 200–299 `status`; optional `body` is at most 1024 bytes.
  TCP probes have no HTTP fields. Command and remote probes are not supported.
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
  This declaration does not turn service-local state into a backup guarantee.
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
  they do not implicitly start a service or add a dependency edge.
- **Bindings:** each name selects `{"endpoint":{"role":"relay","listener":"control","scheme":"http"}}`,
  `{"executable":{"package":"jq","path":"/usr/bin/jq"}}`,
  an artifact/application executable reference under `executable`, or a bundle
  reference. Endpoint schemes are `http` and `tcp`; builtin HTTP's listener name
  is `http`. An enabled consumer cannot bind an unplaced endpoint producer.

Custom `configs` map names to `source` under `deploy/files/` and `mode`:
`immutable`, `seed-once`, `recreate`, or `rendered-json`. Per-start preparation
rebuilds `recreate` files; `seed-once` retains the initial seed across starts.
Only `recreate` and `rendered-json` accept `bindings`, a map from JSON pointers to
typed listener address/port, bundle, binding or copy-number values. Do not insert
secrets here. Custom `environment` is an explicit map of nonsecret typed argv
values; reserved process/application names and `_FILE` names are rejected.

### Profile field reference

Profile 3 retains the existing profile fields, adding `placement` and `listeners`.
`placement` must name every role, with HTTP exactly 1 and others 0 or 1. All
prerequisites of an enabled role must also be enabled. The builder validates the
profile architecture against artifact platform coverage.

The profile `listeners` map uses `<role>.<listener>` keys for exactly the enabled
public custom listeners. Loopback listeners have no profile entry. Each has:

- `ports`: exactly one port, 1–65535, unique across the profile; 22, 80 and 443 are
  reserved. This is the public port, not a service's allocated private port.
- `source_cidrs`: 1–64 unique canonical CIDRs. Each address family must have a
  corresponding public bound address. Choose the actual permitted clients;
  `0.0.0.0/0` is an intentional allow-all IPv4 policy, not a safe default.
- `dns`: `mode: "external"`, a `hostname`, 1–16 literal `public_addresses`, and
  `proxied: false`. Addresses must be bindable on the host. The profile does not
  provision interfaces or upstream NAT.

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

This consumer fixture loads the two HTTP-role bindings from the worked example:

<!-- compiled-source: tests/fixtures/consumer/hostcapabilities/bindings.go#host-capability-bindings -->
```go
// RelayDependencies contains the private control endpoint and tool used by the app.
type RelayDependencies struct {
	// Control addresses the relay's private HTTP control listener.
	Control deployment.Endpoint
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
	return RelayDependencies{Control: control, JSON: jsonTool}, true, nil
}
```

Inject `RelayDependencies` into the component that calls the relay. `Control.URL()`
returns a `url.URL` with the allocated private host/port; append control paths with
the URL API. `JSON.Path` is an absolute verified path, not a PATH lookup;
execute it with an argument vector when the application needs jq for JSON processing.
`Executable` also carries `SHA256`. `Bundle(name)` returns a `BundleDirectory`
with its service-local `Path` and `SHA256`.

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
requires generation 12. On an already bound older host, use its existing legacy
profile to review and apply the forward-only tool upgrade **before** switching
the consumer to schema 3. Do not use a feature profile to request an older host's
upgrade. A fresh host gets the pinned generation-12 tools during bootstrap.

```sh
ENVIRONMENT=production ACTION=tools-plan make deploy
ENVIRONMENT=production ACTION=tools-apply PLAN_TOKEN='<reviewed-token>' OPERATION='<stable-uuid-v4>' make deploy
```

Keep the operation UUID for a tool-upgrade resume. Review the plan; the apply
command asks for confirmation. Tool generation is independent of the framework's
semantic version: verify the selected bundle reports generation 12, rather than
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
must be closed before first activation and after stop. Restart resumes the selected
instance through admission. Report lists the listener under its instance with
exposure and DNS observations. Read status after a lost SSH connection; a lost
reply does not establish whether the operation completed.

### External DNS and Docker-host NAT

Create external DNS-only A/AAAA records for the listener hostname, pointing at the
intended reachable addresses. Raw TCP is not HTTP: do not enable a provider's HTTP
proxy on this name. `proxied: false` records that requirement; DNS answers alone
cannot prove the provider configuration or a completed global cutover. The
preview does not create, update or delete these application records. Certificate
DNS challenges remain the separate existing certificate owner's responsibility.

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
container. The preview neither publishes Docker ports nor configures an upstream
router. Do not add Docker port publishing or DNAT over a Foundry-owned port in the
same observed namespace. Test reachability from a peer namespace or another
machine, not from the host loopback; verify permitted traffic reaches the relay
through nginx stream and that stop closes it.

### Maintenance holds and shared application state

Use deploy `down` for immediate fencing of supervised service listeners. Direct
`artisan down` and maintenance changes from another host only update the shared
application record. The next privileged instance action reconciles that record
through the owned lifecycle operation. A host report identifies stopped-policy
services that are still admitting while the shared record is down.

Held copies are reported separately from failed or unexpectedly inactive services.
HTTP uses its maintenance liveness contract while paused. Deploy, configuration,
restart and boot retain persistent holds; an unavailable shared store never clears
them. `up` removes markers only under pending admission after closure is proven.
If readiness then fails, the operation reports failure and closes the affected
services; it does not claim the application pause was restored. After cleanup
proves closure, an explicit `up` can retry the held selection.
