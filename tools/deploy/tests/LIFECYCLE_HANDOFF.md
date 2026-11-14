# B24 release 1 lifecycle handoff

This reference records the lane A interfaces and the network changes needed for
service-only secret rotation, rollback, and maintenance. These operations remain
capability-gated until their complete transitions are implemented.

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

## Network changes required from lane C

The current `listeners.feature_close` requires all of these conditions:

- The affected-unit set equals every candidate unit.
- The old and candidate unit maps are identical.
- The candidate private revision equals the current registry revision.
- Every candidate endpoint is unused after all units exit.

`listeners.feature_open` requires every declared role to be enabled and running.
Those conditions prevent selective rotation, maintenance with retained roles,
role removal, and rollback to a legacy release.

The required behavior follows the design's reviewed transitions and retained-role
closure contract:

1. `feature_allocate` preserves unaffected endpoints for a service-scoped edit.
   HTTP keeps its allocation when its effective inputs are unchanged. An affected
   backend receives a fresh allocation when old references prevent safe reuse.
2. `feature_close` accepts the explicit `HostTransition.affected_units`, including
   retired units. It withdraws affected ingress before stop, then proves affected
   backend exit and old-reference isolation. Ingress withdrawal alone never grants
   a validation permit.
3. Closure evidence binds the unchanged retained units through
   `ClosureEvidence.retained_units_sha256`. Retained units keep their processes,
   endpoints, and effective inputs. Their sockets do not count as unexpected
   candidate listeners.
4. The current private revision in `HostTransition` identifies the pre-transition
   revision. A configuration candidate can reference the next revision. Lane A
   validates and journals private publication before admission. The network owner
   does not restore credentials or require old and candidate revisions to match.
5. `feature_open` verifies the complete enabled, unheld set using actual systemd
   enablement. Disabled or held roles remain closed. A retained admitted process
   can use its prior invocation record only when lane A proves its effective
   inputs are unchanged.
6. Old and candidate inventories can differ. Closure covers removed units and
   listeners. Retained legacy rollback keeps sticky admission and capable-client
   requirements while using the target's legacy application projection.

Lane C continues to own firewall and nginx writes. Lane A owns private intents,
configuration publication, retained-invocation admission, maintenance markers,
unit installation, and transition completion. No secret values or secret-derived
hashes cross into public plans or network observations.

The topology-2, profile-3, and application binding contracts remain unchanged.
Any additive internal network call parameters need agreement before the selective
lifecycle callers are connected.
