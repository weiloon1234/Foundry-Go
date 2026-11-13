# B24 milestone 4: lane C handoff

The implementation and review fixes use the frozen generation-12 records without modifying
`contracts.py`, `topology.py`, `protocol.json`, lifecycle/provisioning writers or
B26 upload declarations/templates. The fix-round request explicitly authorizes the targeted unit tests and deploy-tools gate.
This file describes integration work for lane A; it is not real-host acceptance evidence.

## Lane A call order

Hold the existing host lock, then the instance lock. Persist operation intent and
the exact affected units/addresses before effects. All returned observations are
evidence, never permission to start a process or commit admission.

1. Parse the frozen `DeploymentProfile` and `RuntimeResolution` records.
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
7. `certificates.ensure_hostnames(host, registry, entry, profiles)` extends the
   existing lineage using active, retained and pending profiles. It retains old
   SANs, stays in the existing explicitly scoped DNS-01 zone and records its SAN
   intent privately. It performs no application A/AAAA writes. Use
   `listeners.observe_dns` / `external_dns` for exact external A/AAAA instructions
   and observations. Resolver agreement is neither DNS-only provider proof nor
   global cutover. Keep external operator confirmation in the reviewed operation.
8. After candidate health and fresh authority, call `edge.open_runtime`, verify
   `certificates.verify_network_consumers`, then `firewall.open_runtime` last.
   Supply the closed firewall digest and a synchronous `authorize` callback that
   re-observes lane A's committed admission or live finalization authority, permitted
   process identities, exact resolution and health. Both opening functions invoke
   it before and after effects. Journal/observe completion separately. Any exception
   or lost result remains pending; cleanup withdraws both owners. Certificate checks
   use fresh TLS connections and compare the served leaf to the owned lineage.
9. `report.declared_network(host, profile, entry=..., resolution=..., closure=...,
   dns_observations=...)` returns safe listener, firewall, DNS, TLS and closure-scope
   findings. Without a supplied resolution it reports nginx ownership as unverified.
   Recorded closure is explicitly historical, never a fresh permit. Existing
   `instance_report` exposes basic declaration findings for profile 3; lane A should
   supply its selected records for the richer report during lifecycle integration.

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
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_network_review test_network test_edge test_certificates test_report test_uploads
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
