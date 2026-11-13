# B24 milestone 4: lane C handoff

The implementation uses the frozen generation-12 records. The authorized plain TCP
extension changes only the public-listener topology check: `tls` accepts `edge` or
`none`, both with `authentication: true`. `contracts.py`, `protocol.json`,
lifecycle/provisioning writers and B26 upload declarations/templates are unchanged.
This file describes integration work for lane A; it is not real-host acceptance evidence.

## Lane A call order

Hold the existing host lock, then the instance lock. Persist operation intent and
the exact affected units/addresses before effects. All returned observations are
evidence, never permission to start a process or commit admission.

1. Parse the `Topology`, `DeploymentProfile` and `RuntimeResolution` records.
   Keep each selected/retained/pending profile paired with its own topology.
   `listeners.public_policies(profile, topology)` validates that pairing and resolves
   public modes from the declaration; address bindings never imply a TLS mode.
   `listeners.public_claims(profile)` and `private_claims(instance_id, resolution)`
   produce concrete TCP/family/address/role/copy claims. Check new claims with
   `require_available`; allocate private `(role, listener)` pairs with
   `allocate_private`. Supply every instance's current/pending/retained reservations,
   live `observe_sockets(host)` results, `edge.retained_references` for feature
   instances, and the host-wide `edge.retain_global_references` inventory. Wrap
   upstream `SocketIdentity` values in `ObservedSocket(socket, ())` for allocation.
   A public port retained by the same instance is an existing reservation, not a
   new claim. Do not discard it while forming the old/candidate union.
2. Public TCP adoption requires an explicit reviewed firewall opt-in. Plan using
   `firewall.observe(..., integrated=False)`. Pass its digest as
   `reviewed_sha256` to `install_closed`. This adds only an empty owned chain and
   jump to each UFW before file, then reloads and observes the closed result.
   Show the literal `persistent_projection` edits in the plan. A reload can close
   other feature instances' runtime allows; reopening them requires their own
   fresh admission/health evidence. Do not restore saved runtime rules.
3. Plan `libnginx-mod-stream` through the package owner. Use
   `edge.edge_findings(profile.projection(), directory, topology=topology)` for the
   combined HTTP edge and listener plan findings, or `edge.listener_findings(profile,
   topology=topology)` for just TCP. Plans explicitly label plain listeners
   **unencrypted** and state that the service must authenticate every client.
   Use
   `edge.install_stream_include(host, reviewed_nginx_sha256=...)` only with the
   reviewed digest of `/etc/nginx/nginx.conf`. It requires the existing top-level
   `/etc/nginx/modules-enabled/*.conf` include. It does not rewrite nginx.conf.
   Render the owned HTTP site through the existing renderer with the selected
   resolution's HTTP address, then pass the text through `edge.gate_http` for
   feature-adopted instances, including feature-to-legacy rollback. Existing
   upload/proxy directives are preserved. Never apply this gate to untouched
   legacy instances. Bind the complete HTTP/stream projection into the existing
   `RuntimeResolution.nginx_sha256`; edge does not allocate another HTTP address.
4. Before candidate starts, call `firewall.withdraw` for the affected old/candidate
   public socket union, then `edge.withdraw` for the affected roles. Selective
   operations pass `retained_claims` and their `bindings` to firewall functions;
   those allow rules and unrelated nginx routes are retained. Observe both owners
   again after withdrawal and pass their records plus the complete public claim
   union to `listeners.ingress_evidence`. Empty-public-listener scopes require no
   UFW integration. Persist only `ingress_withdrawn` at this point.
5. Stop/prove exit of every old affected workload, job, control process and cgroup
   member in lane A. Prove unchanged UnitSpecs/inputs/allocations for retained
   units. Call `listeners.complete_closure` with that fresh, scoped exit digest,
   the candidate reservations and **all** possible old upstream references,
   including the host-wide inventory and all instance inventories. The function
   refuses candidate address reuse and returns the frozen `ClosureEvidence` at
   `application_admission_closed`. The exit digest must describe this operation,
   boot, resolution and exact unit scope; an arbitrary prior digest is not proof.
6. Lane A re-observes these conditions when granting/consuming validation permits.
   It alone verifies executor/process/invocation identities, binds permits to the
   closure ID and validates started candidates against those permits. No candidate
   may start between steps 4 and 5. Reboot discards live proofs and requires a new
   whole-instance scope; selective live proofs do not become boot authority.
7. `certificates.ensure_hostnames(host, registry, entry, profiles, topologies=topologies)`
   extends the existing lineage using only `tls: edge` listeners in the active,
   retained and pending profiles. Supply the matching topologies in the same order;
   mismatched counts are rejected. An all-plain set performs no certificate work.
   It retains old
   SANs, stays in the existing explicitly scoped DNS-01 zone and records its SAN
   intent privately. It performs no application A/AAAA writes. Use
   `listeners.observe_dns` / `external_dns` for exact external A/AAAA instructions
   and observations. Resolver agreement is neither DNS-only provider proof nor
   global cutover. Keep external operator confirmation in the reviewed operation.
8. After candidate health and fresh authority, call `edge.open_runtime(...,
   topology=topology, authorize=...)`, verify `certificates.verify_network_consumers`
   for TLS consumers, then `firewall.open_runtime` last. An instance that has never
   adopted TCP TLS has no network certificate claim to verify. Its existing HTTP
   certificate checks remain owned by the HTTP lifecycle.
   Supply the closed firewall digest and a synchronous `authorize` callback that
   re-observes lane A's committed admission or live finalization authority, permitted
   process identities, exact resolution and health. Both opening functions invoke
   it before and after effects. Journal/observe completion separately. Any exception
   or lost result remains pending; cleanup withdraws both owners. Certificate checks
   use fresh TLS connections and compare the served leaf to the owned lineage.
9. `report.declared_network(host, profile, topology=topology, entry=..., resolution=..., closure=...,
   dns_observations=...)` returns safe listener, firewall, DNS, TLS and closure-scope
   findings. Without a supplied resolution it reports nginx ownership as unverified.
   Recorded closure is explicitly historical, never a fresh permit. Existing
   `instance_report` exposes basic declaration findings for profile 3; lane A should
   supply its selected records for the richer report during lifecycle integration.

## Checkpoint-2 adapters (`3897443a`)

Lane A's unchanged `feature_network.py` can call these functions in `listeners.py`:

| Adapter | Existing owners and returned evidence |
| --- | --- |
| `feature_allocate(host, entry, topology, profile, operation_id)` | Read the host registry, immutable launch inventories, socket owners and complete nginx references; call `require_available` and `allocate_private`; return endpoints and the canonical digest of the existing HTTP/upload renderer plus `stream_projection` |
| `feature_close(host, entry, transition, resolution, units)` | Validate the selected `HostTransition`, `RuntimeResolution` and exact role-to-unit dictionary; withdraw old/candidate firewall claims and HTTP/stream routes; observe systemd jobs, main/control PIDs, descendant cgroups and admitted helpers; drain old nginx worker identities; call `ingress_evidence` then `complete_closure` with fresh exit and reference-isolation observations |
| `feature_open(host, entry, resolution, evidence)` | Revalidate the selected admission receipt and live permit or committed authority through `AdmissionStore.preparation`, call `services.wait_ready`, prove private socket ownership, ensure TLS names, restore the selected HTTP projection, call `edge.open_runtime` then `firewall.open_runtime`, and return `True` only after final nginx/socket/firewall observations |

Callers still hold the host and instance locks. Launch records are read from lane
A's `runtime/<resolution-id>/launch.json` through its protected-file reader and
validated by its `UnitInventory`. Allocation writes nothing, including reference
receipts. Legacy HTTP allocations and every retained candidate remain reserved.
The second allocation result is a SHA256, as required by the seam, not raw nginx
text. No frozen record fields or lane A source files change.

Checkpoint 2 passes whole-instance transitions. These adapters reject partial
scopes, changed retiring role sets, foreign units, incomplete history and stale
selection rather than broadening a supplied scope. Closure does not stop workloads
or kill nginx workers. It waits up to ten seconds for the old worker generations;
remaining sessions, including those for other instances, cause refusal. This is
conservative and can delay closure on a shared busy nginx. Closed owned HTTP
renderings replace backend proxy targets with 503 responses; successful opening
restores the exact renderer output, including B26 upload directives. A drained
same-resolution restart uses the fresh complete nginx reference inventory;
allocation still quarantines historical references.

Reviewed UFW adoption, the stream include/module and an enabled owned HTTP site
remain prerequisites. The adapter has no review digest with which to approve
these installations itself. Fresh boot ordering/preparation still belongs to lane
A; these functions do not replace its coordinator or start units. Any opening
failure during effects withdraws both network owners. All-plain histories perform
no TCP certificate work; selected TLS consumers must pass the effective leaf and
handshake postcondition before the public firewall opens.

There is also an upstream checkpoint-2 registry blocker: its `Host.read_registry`
calls `contracts.validate_registry`, which calls `identity`/`validate_profile` and
still rejects schema 3. Registry topology validation likewise uses the legacy
validator. Lane A/integration must resolve that boundary; these adapters do not
bypass it or change the frozen contracts. The tests explicitly supply a fixture
registry reader, so passing them does not claim full registry/lifecycle acceptance.

`test_feature_network_adapters.py` calls the unedited checkpoint seam and admission
owners in [the checkpoint fixture](network_fixtures/lane_a_3897443a/README.md).
It covers read-only reservations/projection hashing, exact scope, active jobs and
control processes, cgroup descendants, live admitted helpers, old nginx workers,
old references, same-resolution restart, pending and committed opening, dead
executors, foreign socket owners, TLS postconditions and withdrawal on failure.

Native verification for this seam change (no Go tests or host actions):

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_network test_network_real test_network_review test_network_preview test_feature_network_adapters test_edge test_certificates test_report
```

Result: 136 tests passed. From the repository root, `make deploy-tools-check`
ran 714 tests: 704 passed, 9 skipped, and only the known
`test_candidate_python_inventory_contains_only_runtime_modules` failure remained
(`lib/listeners.py` and `lib/firewall.py` missing from the integration inventory).
All three checkpoint fixture files were compared byte-for-byte with `git show
3897443a:tools/deploy/lib/<file>`; the frozen contracts also match that checkpoint.

## Plain public TCP

`edge.stream_projection(profile, resolution, lineage, topology=topology)` and
`open_runtime` use one stream rendering path for both modes. Plain listeners omit
`ssl` and every `ssl_*` directive; `lineage` may be `None` when all listeners are
plain. nginx still owns the public IPv4/IPv6 sockets and proxies to allocated
loopback endpoints. CIDR access rules, default deny, runtime-only exposure,
per-instance boot closure, stale-worker reference retention and closure evidence
are identical. The service must never bind directly to the public address.

Plain TCP carries application traffic unencrypted, including any credentials the
application sends. `authentication: true` acknowledges the service's responsibility
to authenticate clients; it does not enable nginx authentication. No protocol-specific
handling is added. Reports include each listener's `tls` and `authentication` policy
and the same unencrypted/service-authentication warning as plans.

Plain listener hostnames never enter new certificate claims, SAN requests or TLS
consumer probes. Retained TLS generations still retain their SANs for rollback;
switching a selected listener to plain does not delete a previously owned SAN.
Renewal probes skip its plain runtime projection. Ordinary HTTP certificates and
TLS listeners retain their existing issuance and effective-renewal checks.

## Boot and retention

Install the nginx drop-in from `foundry-edge-boot.conf` with lane A's fixed boot
coordinator unit name. The coordinator runs after UFW restoration and before nginx
reads configuration. For each feature instance, `edge.prepare_boot` removes only
its runtime projection. Missing/pending/invalid admission must leave that instance
closed without failing the shared coordinator for other instances. The persistent
HTTP map defaults to 503; the persistent stream include has no listeners without
runtime files. Generic nginx and certificate reloads cannot reconstruct routes
from a release archive.

The coordinator must reconstruct whole-instance closure before grants. A later
per-instance health coordinator opens only admitted enabled units. `prepare_boot`
does not stop workloads or prove application closure. Same-boot calls retain
quarantined addresses. Historical nginx references are conservatively retained
until a new kernel boot; exhausting the pool refuses allocation. On initial
adoption, host-wide reference capture requires existing nginx workers to drain
naturally after a validated reload. It never kills foreign sessions. Dynamic/DNS
upstreams and an undrained initial generation refuse address-isolation proof.

## Supported firewall layout

The backend supports active UFW on both iptables-nft (Ubuntu's default) and
iptables-legacy, without switching alternatives. It validates the complete IPv4
and IPv6 kernel views, family-specific `ufw-*`/`ufw6-*` chains, DROP input/forward
policies, literal settings and reviewed Ubuntu UFW reload machinery. It reconstructs
all standard helpers and applies the before/after/user chain-reset order, then
appends UFW's user hooks. See `network_fixtures/ufw/README.md` for source provenance.

For nft, every table, chain, hook, policy and per-chain rule count must agree with
the iptables compatibility views. Decoder diagnostics, unrelated native tables,
extra hooks/objects, incompatible expressions and unstable observations refuse.
Legacy accepts no concurrent native nft policy. Both backends refuse foreign
broader/interface/routed allows, NAT/raw/mangle rules and custom UFW init hooks.
They never enable/reset UFW, switch alternatives, or remove foreign rules.

Nginx reference capture tokenizes complete directives and expands dumped includes
in their actual contexts. Multiline/quoted targets, repeated included upstream
fragments, and separate HTTP/stream namespaces are handled. Missing dump members,
dynamic or DNS references, programmable handlers and ambiguous syntax refuse the
whole observation. No partial inventory can become closure evidence.

TCP adoption upgrades the known owned renewal service with an independent
`ExecStartPost` check. Legacy consumers accept that strengthened shared service,
and provisioning another legacy instance preserves it. Issuance separately checks
effective consumers after Certbot, even if its deploy hook failed while Certbot
returned zero. Safe per-lineage observation receipts retain pending/failed/verified
status. Reports expose failures, including after feature-to-legacy rollback.

Reporting first validates one host-wide public claim inventory. The host scanner
exempts exact claimed TCP addresses/ports, not every socket with the same port.
Per-instance findings identify that instance's ownership problems; unclaimed
TCP/UDP exposure remains a host-level finding.

## Unit commands and M6 fixture

The fix-round acceptance commands (run from the indicated directories):

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_network_real test_network_review test_network test_plain_tcp test_edge test_certificates test_report test_uploads
# From repository root:
make deploy-tools-check
```

`acceptance_network.py` is opt-in and never discovered by the ordinary unit suite.
Its module docstring defines the prepared container case. The fixture supplies a
closed integrated firewall, fixed topology/profile/old/candidate records, installed
SAN lineage and HTTP gate, plus permitted/denied independent network namespaces
with IPv4 and IPv6 paths. Allocate fresh backend addresses per scenario. Use the
existing synthetic certificate opt-in and ownership marker; no real DNS-01 provider
access is part of these scenarios.

The scenarios exercise new-connection denial before opening, both source policies
and address families, UFW reload closure, an established TLS session surviving
graceful nginx withdrawal, fresh backend isolation and effective synthetic renewal.
The M6 lifecycle harness still owns cgroup/permit/executor faults, actual reboot,
mixed-instance continuity, deployment/configuration rollback and generation-9/10
upgrade sequences. The standalone echo backends do not substitute for systemd
exit/invocation acceptance.

## Review fix-round results

All six findings have regression coverage in `test_network_review.py`:

| Finding | Implementation | Primary regression |
| --- | --- | --- |
| Default iptables-nft rejected | `lib/firewall.py`: validate nft objects against complete compatibility views; preserve legacy support | `test_default_ubuntu_nft_layout_is_observed_closed` |
| Partial nginx reference inventory | `lib/edge.py`: complete tokenization, include-context expansion and whole-observation refusal | `test_multiline_quoted_and_included_upstreams_are_reserved`; `test_unresolved_multiline_reference_refuses_instead_of_partial_inventory` |
| Wrong IPv6 namespace | `lib/firewall.py`: carry family through projection, observation and ownership checks | `test_standard_ipv6_projection_targets_ufw6` |
| Missing UFW helper graph | `lib/firewall.py`: validate reload machinery and reconstruct its full helper/restore graph | `test_standard_reload_helper_graph_is_observed_closed` |
| Certbot zero masks failed TLS hook | `lib/certificates.py`, renewal service template: independent issuance and `ExecStartPost` checks with failure receipts | `test_certbot_zero_after_failed_hook_does_not_verify_issuance`; `test_renewal_service_enforces_postcondition_after_certbot_zero` |
| Cross-instance report misclassification | `lib/report.py`: validated host claim inventory, exact socket matching and scoped ownership findings | `test_other_instances_claim_is_not_unexpected_and_host_scanner_uses_inventory` |

Seven core regressions replayed against `34cd3be3` all failed (four assertion
failures and three refusals/API errors), reproducing all six findings. The targeted
command above then passed all **116 tests** on the fix-round source.

`make deploy-tools-check` ran **681 tests: 671 passed, nine skipped, one failed**.
The failure is
`test_upgrades.UpgradeTests.test_candidate_python_inventory_contains_only_runtime_modules`:
lane A's `lib/upgrades.py` `REQUIRED_FILES` inventory omits `lib/firewall.py` and
`lib/listeners.py`. Both files already exist in `34cd3be3`. Replaying that check with
the unchanged `34cd3be3` upgrade module reproduces the identical failure. The
inventory and its test were not modified because they are outside lane C.

`git diff --check` passed. No live firewall/nginx, DNS, provider, alternatives,
package-installation or commit actions were performed. Public Ubuntu package files
were downloaded into task-owned temporary paths solely as source references.

## Live Ubuntu UFW observation

The second review uses the operator's unedited Ubuntu 24.04 nft, 26.04 nft and
24.04 legacy [captures](network_fixtures/ufw-real/README.md). `test_network_real.py`
reproduces the non-executable installed hooks, generated logging inserts and a
real-world nginx regex location before fixing them. Non-executable, owned hooks
are inspected and included in the observation; executable, writable or symlinked
hooks still refuse. Insert commands replay at their actual chain position,
including an omitted position's default of 1. Foreign ACCEPT rules still refuse.

nginx tokenization follows
[`ngx_conf_read_token`](https://github.com/nginx/nginx/blob/master/src/core/ngx_conf_file.c):
regex escapes such as `\.` remain intact in unquoted, single-quoted and
double-quoted arguments. Only nginx's known quote, backslash and n/r/t escapes
are decoded. Quoted token endings, embedded quotes and comment boundaries follow
the same lexer; unresolved upstreams still refuse the entire reference inventory.

Run this command **from the repository root** to prepare a new disposable container
and run only the read-only observation scenario. It needs a Docker engine with
privileged Linux containers and IPv6/netfilter support. It mounts the deploy bundle
read-only, does not use the host network, and never switches iptables alternatives.
The setup commands change only the disposable container's network namespace.

```sh
docker run --rm --privileged \
  --sysctl net.ipv6.conf.all.disable_ipv6=0 \
  -e DEBIAN_FRONTEND=noninteractive \
  -e FOUNDRY_DISPOSABLE_HOST=1 \
  -e FOUNDRY_UFW_OBSERVATION_ACCEPTANCE=1 \
  -v "$PWD/tools/deploy:/opt/foundry-deploy:ro" \
  ubuntu:24.04 bash -euxc '
    apt-get update
    apt-get install -y --no-install-recommends python3 ufw iptables nftables iproute2
    ip link add fdy-test type dummy
    ip link set fdy-test up
    ip address add 203.0.113.10/32 dev fdy-test
    ip -6 address add 2001:db8::10/128 dev fdy-test
    ufw default deny incoming
    ufw allow 22/tcp
    ufw allow 80/tcp
    ufw allow 443/tcp
    ufw logging low
    ufw --force enable
    python3 -B /opt/foundry-deploy/tests/acceptance_network.py UFWObservationAcceptance -v
  '
```

Replace `ubuntu:24.04` with `ubuntu:26.04` to check that release. The scenario uses
whichever supported backend the image already selects. A preconfigured legacy
image can run the same scenario without any alternatives-switch command.

Unlike the other M6 scenarios, `UFWObservationAcceptance` needs no systemd,
case.json, certificate or external DNS. It checks installed hook modes and shared
allow ports, calls `firewall.observe(..., integrated=False)` against the live
IPv4/IPv6 and nft JSON views, and checks that persistent files/modes are unchanged.
It never installs Foundry chains, reloads UFW or opens a listener. This scenario
has been written but was not run as part of the no-host-actions fix round.

## Live network preview before lane A integration

`NetworkPreviewAcceptance` is a self-contained opt-in fixture in
`acceptance_network.py`. It reuses the existing systemd host image, adding nginx,
`libnginx-mod-stream`, UFW, iptables and nftables. No provider, DNS writer, Go build,
release deployment is involved. The application-closure scenario calls the
unedited checkpoint-2 `feature_network.py` seam; the other scenarios exercise
lane C's network primitives. Public lane C function signatures are unchanged.

The fixture creates a veth-connected peer namespace with IPv4/IPv6 addresses,
configures default-deny UFW with only 22/80/443 allowed, and generates a self-signed
certificate. TLS clients trust that certificate and verify the hostname; verification
is never disabled. A private echo backend returns the exact probe bytes. This tests
transport, not application authentication: the real service must still authenticate
clients as acknowledged by `authentication: true`.

| Scenario | Real observations and assertions |
| --- | --- |
| Plain and TLS opening/withdrawal | Default-deny proof before backend startup; peer TCP connection failure while nginx is listening but UFW is closed; exact bytes after runtime opening over both families; new connections denied after withdrawal |
| Stale sessions, plain and TLS | The old session survives graceful withdrawal; evidence remains `ingress_withdrawn` with no exit/isolation proof; completion without exit proof refuses; fresh endpoints avoid retained/global references; stopping the old echo fixture terminates its session before reopening |
| Boot cleanup primitives, plain and TLS | Stop nginx and the old fixture; reload UFW to discard runtime allows; call `edge.prepare_boot`; restart nginx and prove closed; reopen a fresh backend and prove both families reachable |
| Reports | Validate nginx cgroup/socket ownership against the instance claim inventory; flag an undeclared public binding; exclude the declared port from unexpected exposure; attribute absent listeners after withdrawal to the instance and report historical ingress-only evidence |
| Application closure adapter | Run real systemd echo units with checkpoint-2 launch/transition records; refuse closure while the units live; stop them, drain old nginx workers, and emit scoped `application_admission_closed` evidence from real kernel and nginx observations |

One test is explicitly skipped pending lane A:

- `test_boot_unit_coordination_by_lane_a`: lane A owns the actual coordinator,
  systemd ordering and fresh permits. The primitive test calls `prepare_boot`
  directly, without installing a substitute boot unit. A reload does not change
  the kernel boot ID; same-boot reference reservations remain intact.

The primitive opening scenarios' `authorize` callback verifies only echo backend
health, not lane A admission. The adapter closure scenario uses a protected fixture
registry reader and the unedited checkpoint-2 owners; it does not bootstrap a real
application or issue start permits. Adapter opening with pending and committed
admission is covered by native unit tests; full live lifecycle opening and boot
coordination remain integration requirements. This harness does not claim
end-to-end lifecycle acceptance.

Run these **Bash commands from the repository root**. They reuse `host.Dockerfile`
for systemd setup, use private network/PID/cgroup namespaces, and mount no host
cgroup filesystem. They require privileged Linux containers, cgroup v2 and systemd,
as does the existing deployment harness. No alternatives switch is performed.

```bash
set -e
for version in 24.04 26.04; do
  docker build --build-arg UBUNTU_VERSION="$version" \
    -f tools/deploy/tests/host.Dockerfile \
    -t "foundry-network-base:$version" tools/deploy/tests
  docker build --build-arg BASE_IMAGE="foundry-network-base:$version" \
    -f tools/deploy/tests/network-preview.Dockerfile \
    -t "foundry-network-preview:$version" tools/deploy/tests
  container="foundry-network-${version//./-}-$$"
  docker run -d --name "$container" --privileged --cgroupns=private \
    --memory=2g --cpus=2 --sysctl net.ipv6.conf.all.disable_ipv6=0 \
    --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
    --mount "type=bind,source=$PWD/tools/deploy,target=/verification/deploy,readonly" \
    "foundry-network-preview:$version"
  for attempt in $(seq 60); do
    docker exec "$container" test -S /run/systemd/private 2>/dev/null && break
    sleep 1
  done
  docker exec "$container" systemctl start multi-user.target
  docker exec -e FOUNDRY_DISPOSABLE_HOST=1 -e FOUNDRY_NETWORK_PREVIEW=1 \
    "$container" python3 -B /verification/deploy/tests/acceptance_network.py \
    NetworkPreviewAcceptance -v
  docker cp "$container:/run/foundry-network-acceptance/preview" \
    "./network-preview-$version-$$"
  docker rm -f "$container"
done
```

Expected live result: eight passing network scenarios and the named lane A boot
skip. Ingress evidence, adapter closure and a report are copied out before successful containers
are removed. A failure leaves the container intact (`$container`) for inspection;
check `journalctl -u nginx` and copy the preview evidence before removing that exact
container. Each rerun requires a fresh container.

These commands have not been executed in this no-host-actions session. Native
unit checks validate fixture records and opt-in guards, not live network behaviour.
