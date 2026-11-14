# B24 release 1 lifecycle handoff

This reference records lane A's private configuration and unit-graph interfaces.
Service-secret review, delivery, rotation, target-owned rollback and application
configuration revisions, maintenance and generation-13 recovery are implemented.
The recovery callback and isolation contracts are in [RECOVERY_HANDOFF.md](RECOVERY_HANDOFF.md). The live network scenarios are opt-in and have not run locally.

## Lane A interfaces available in this worktree

`runtime_config.service_secret_snapshot_paths(host, entry)` returns the current
private service-secret index and its exact snapshot paths. The result includes
retained secret names from earlier releases. It is input for private backup or
recovery, never a public report. An instance without an index returns an empty tuple.

`feature_lifecycle.stop_units(host, inventory, names=None)` stops the explicit
selection in reverse dependency order and proves process and cgroup exit. An
omitted selection means the whole graph. It does not change enablement.

`feature_lifecycle.start_units(host, inventory, names, deadline)` starts an explicit
enabled selection in dependency order. It checks retained prerequisites before
starting dependents and checks each started role's readiness. It never enables a
disabled unit. The deadline is an absolute `time.monotonic()` deadline.

The caller owns the host lock, instance lock, transition journal, admission permit,
and network closure. These graph functions do not grant admission or open traffic.

## Systemd owns enablement

`services.enabled_units(host, units, timeout=5)` reads systemd. Stored
`DeploymentSelection.enabled_roles` is an audit projection. The admission gate
also checks systemd for committed starts. Pending starts still require an explicit
validation permit and proven closure.

The next privileged instance action migrates a committed preview selection by
enabling its currently running units without starting anything. It does not enable
stopped units. The private migration receipt records the adopted roles and is
included in the status response. Pending deployments do not migrate automatically.

Workload units retain `WantedBy=multi-user.target` and their declared `After=`
ordering. The boot exposure coordinator runs after the workload units and verifies
the enabled set. It does not start workloads. Declared prerequisites do not add
systemd `Wants=` or `Requires=` dependencies.

## Scoped network operations

Lane A now owns the lifecycle adapters in `listeners.py`; the earlier network
lane is complete. Current backup/recovery lane C does not edit the adapters.

- `feature_allocate(..., retained=...)` preserves unchanged endpoints and allocates
  fresh affected backends around live and retained references.
- `feature_withdraw` records only `ingress_withdrawn` for the affected old/candidate
  address union. Unchanged unit identities and runtime routes are fingerprinted.
- `feature_close` additionally proves affected backend exit and reference isolation.
  The whole-instance call shape remains supported. Validation permits cannot
  exceed the affected unit/address evidence.
- `feature_open` verifies selected process identities and readiness, then changes
  only the affected exposure. Unchanged processes and routes must remain identical.
  Configuration revisions preserve enabled services that were deliberately stopped.

`feature_plan.plan` collects new declared service secrets before activation.
`runtime_config` retains immutable private intents, current secret selection and
configuration revisions. Public plans carry opaque intent IDs and redacted key
names; secret values and their hashes remain private.

`acceptance_network.py` contains opt-in TCP and TLS scenarios for selective
withdrawal, an unchanged established connection and selective reopening. These
network scenarios do not fabricate application/cgroup closure evidence.

The topology-2, profile-3, frozen deployment records and application binding API
remain unchanged. Retained unit origins and exact live invocation receipts are
recorded privately. An origin can remain valid after its workload exits, but it
cannot authorize continuity during a pending transition. A replacement invocation
must consume the normal scoped permit. Boot coordinators pin the recorded HTTP
generation, then validate and load the selected whole-instance inventory.
New resolutions materialize their own environments and compare credential digests
only privately.

## Target-owned release declarations and configuration holds

`release.persist_declaration` records a complete validated topology and profile,
keyed by archive and topology digests. `release.target_declaration` loads that
release's record. Historical legacy imports require an explicitly supplied matching
topology; when the archive digest was not retained, the record identifies its
manifest provenance rather than inventing an archive digest.

Rollback and archive reactivation resolve current private configuration, allocate
fresh affected endpoints and validate schema compatibility without migration or
seeding. `profile_update` carries the selected target projection to the client.
Removed-role enablement is retained privately for reviewed restoration. No rollback
reads a historical secret snapshot.

Reviewed release recovery uses the held running set, filtered by actual enablement
and the target graph, and cannot clear the hold before startup and readiness succeed.
Worker configuration resolves from the target declaration and effective environment.

A published configuration that fails readiness leaves a private configuration hold
and pending admission. Independent cleanup proves closure before corrective config
or a reviewed release can proceed. Ordinary starts and restart cannot clear it.

## Maintenance interface

`feature_maintenance` owns persistent per-copy marker paths, the saved enabled and
running selection, and live shared-store reconciliation. Maintenance prerequisites
are `requires` and endpoint-binding edges only. Executable bindings refer to
installed files and do not depend on a role remaining alive. `continue` is accepted;
existing `serve` declarations retain their original projection and digest.

The graph lifecycle and renderers carry maintenance holds without changing actual
enablement. Independent failed-up cleanup preserves unchanged invocation origins,
allowing an explicit retry after closure while the shared pause remains off.
Recovery callers can use `marker_path`, `publish_markers` and `clear_markers` under
the instance lock; clearing still requires pending admission and proven closure.
