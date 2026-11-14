# B24 release 1 lifecycle handoff

This reference records lane A's private configuration and unit-graph interfaces.
Service-secret review, delivery and rotation are implemented. Rollback,
application configuration changes, maintenance and recovery integration remain
in progress. The live network scenarios are opt-in and have not run locally.

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
remain unchanged. Retained unit origins are recorded privately; new resolutions
materialize their own environments and compare credential digests only privately.
