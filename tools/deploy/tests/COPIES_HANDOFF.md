# Numbered service copies: shared interface

Release 2a, lane A1, turn 2a migrates inventory callers to copy identities. Counts above one still fail
with `unsupported_capability` in `topology.validate_feature_profile`. A1 removes
that gate only in a later count-enablement turn. Wrappers
belong to release 3.

## Identity and expansion

`contracts.UnitIdentity(instance_id, role, ordinal)` is the frozen, ordered key
for unit names, specs, selections, and readiness observations. It already keys
persisted unit specs and admission evidence, so no second copy identity is needed.
Ordinals are integers from 1 through `MAX_ROLE_COPIES` (128). Placement is dense
from 1 through count, with at most `MAX_UNITS` (256) concrete units. Built-ins
remain zero or one; HTTP is exactly one.

The shared helpers are:

- `topology.parse_placement(document)` parses counts and enforces the total bound.
- `topology.expand_placement(topology, instance_id, placement)` returns an ordered
  `dict[UnitIdentity, Role]`, validating built-ins, dependencies, and socket collisions.
- `services.expanded_unit_names(name, topology, profile)` returns
  `dict[UnitIdentity, str]` with canonical systemd names.
- `services.unit_specs(deployment)` returns `dict[UnitIdentity, UnitSpec]`.
- `services.select_role_units(units, role="all", *, roles=None)` returns identities
  in inventory order. A role selector includes every placed copy of that role.
- `listeners.private_listeners(topology, profile)` returns
  `tuple[(UnitIdentity, listener_name), ...]`. This is the input to
  `listeners.allocate_private(instance_id, roles, reserved, observed, ...)`.
- `listeners.unix_endpoints(topology, profile)` returns every placed Unix endpoint.

`UnitInventory.units` and `.specs` are ordinary dictionaries keyed only by
`UnitIdentity`. `.roles` contains role-name-keyed policy. There is no extras map.
Selections, readiness observations, enablement, maintenance holds, and affected
sets use identities internally. Dependency edges expand to every prerequisite
copy under one shared readiness deadline.

## Selection API for A2

- `inventory.copies(role)` returns that role's placed identities in ordinal order.
  An unplaced declared role returns `()`; an unknown role is rejected.
- `inventory.identity(role, ordinal=1)` selects one placed copy; absent copies fail.
- `inventory.select(role="all", *, roles=None, ordinal=None)` returns identities
  in inventory order. `roles` selects a sequence of placed role names. `ordinal`
  requires a single explicit `role` and selects only that copy.
- `inventory.dependencies(identity)` returns direct prerequisite copies.
  `prerequisites(selected)` returns their transitive closure.
- `start_order`, `stop_order`, `start_in_order`, and `named_operation` consume
  identity selections. `named_operation` observations are identity-keyed.
  `render_unit`, `probe`, `wait_ready`, and `network_confinement.isolation` take
  an identity; `wait_ready_set` takes an identity selection.

`topology.instance_units` and `selected_units` also return identity-keyed maps for
legacy profiles. `selected_units(..., role=...)` expands a role selector;
`roles=` accepts an internal identity selection. `services.enabled_units` and
`release.enabled_roles` preserve identity keys.

Existing singleton wire records remain unchanged. At their boundaries,
`services.unit_identities(instance_id, roles)` parses role names, including removed
historical roles. `services.unit_role_names(identities)` emits role names and
refuses non-singleton or mixed-instance identities. Lane A2 extends its record
schemas before persisting selections with higher ordinals. Role-scoped environment
and secret-path records remain role-scoped until lane C changes their owners.

## Endpoints and references

`AllocatedEndpoint(role, listener, address, port, ordinal)` requires an explicit
ordinal in memory. `AllocatedEndpoint.parse` treats an omitted wire ordinal as 1.
`UnixEndpoint` also carries `ordinal`; its parser has the same legacy default.
`record_document` omits ordinal 1 to preserve existing descriptor and resolution
digests. Other ordinals are serialized explicitly.

`contracts.copy_socket_name(name, ordinal)` preserves `name` for copy 1 and uses
`name.<ordinal>` for later copies. `UnixEndpoint.for_listener(..., ordinal=n)`
owns the derived path. Names never change when a count grows or shrinks. Expansion
rejects collisions, including a declared `wl0.2` colliding with copy 2 of `wl0`.
Private endpoint identity is `(role, ordinal, listener)` within a resolution.
Wildcard reservations apply to every copy of the declaring role.

`services.ServicePaths.for_unit` preserves existing copy-1 paths; later copies
use `<role>.<ordinal>` for runtime and persistent state directories. Shared-directory
grants and network confinement remain role policies inherited by every copy.

Arguments and environment use only `{"copy_number": "self"}`, resolved as the
decimal ordinal. Rendered JSON field bindings resolve it to an integer.

`FEATURE_COPIES_GENERATION = 17` is the client and host floor for counts above one.
The existing `copy_number` reference does not raise the floor.
`upgrades.feature_generation` and `check_feature_generation` accept `profile=` for
the candidate profile; without it they inspect `entry["profile"]`. Both preserve
the selected floor through rollback and recovery. Protocol generation is 17.

## Lane ownership

Paths below are relative to the repository root. Each lane owns corresponding
tests and documentation.

| Lane | Files and responsibility |
| --- | --- |
| A1 | `tools/deploy/lib/contracts.py`, `topology.py`, `services.py`, `listeners.py`, `feature_lifecycle.py`, `feature_plan.py`, `upgrades.py`, and `tools/deploy/protocol.json`: shared contracts, profile parsing, inventory, rendering, private allocation, start/stop/readiness, and count transitions. |
| A2 | `tools/deploy/local/`, `tools/deploy/lib/report.py`, `feature_maintenance.py`, `maintenance.py`, `backup.py`, `recover.py`, `feature_recovery.py`, and `recovery_rehearsal.py`: client/CLI `COPY=` selection, reporting, maintenance holds, rollback enablement records, backup, and recovery coverage. |
| C | `tools/deploy/lib/runtime_config.py`, `tools/deploy/lib/edge.py`, and `deployment/bindings.go`: copy-scoped secret intake, `copies` binding delivery, Go `Copies`, and public per-copy ports. |

Shared-file edits need coordination with A1. Lane C owns its additions to topology
binding/secret parsing and public listener profile parsing in `topology.py`, binding
records in `contracts.py`, and public claims in `listeners.py`. Lane A2 owns its
enablement record changes in `contracts.py` and rollback enablement logic in
`feature_lifecycle.py`. Keep allocation, identity, and socket naming in the shared
helpers above.

## A2 operations records (release 2a)

A2 adds `contracts.unit_selection_names(identities)` and
`unit_selection_identities(instance_id, selection)`. These serialize a concrete
copy as `role` for ordinal 1 and `role.N` for ordinals 2–128. Role identifiers
cannot contain dots, so this is unambiguous; explicit `.1`, leading zeroes,
duplicates and mixed-instance selections are rejected. `DeploymentSelection`
accepts these keys in `enabled_roles` and `maintenance_holds`, bounded by
`MAX_UNITS`. Use these codecs for persisted **copy selections**, including
operation intents/plans; retain role names for role policies and resolution.roles.
A2 leaves the old singleton helpers in services unchanged.

A2's provisional removal hook is
`copy_enablement.record_removed(host, entry, removed)` where `removed` maps
`UnitIdentity` to bool. Call under host/instance locks, before disabling/removing
units. A1 may publish its required signature here; A2 will match it. Evidence stays
in the existing root-owned `removed-enablement.json`, keyed by the codec above.
`removed_enablement(host, entry)` reads identity-keyed evidence;
`restoration_plan(host, entry, previous, inventory)` explains absent target copies
with `role`, `ordinal`, `enabled`, and `evidence: recorded|missing`.

A2 integration notes:

- Manager forwards JSON `copy` (integer) with `role` for stop/restart. A1's executor
  must select with `inventory.select(role, ordinal=request.get("copy"))`; this
  branch still rejects named feature operations until A1 lands its lifecycle.
- A2 migrated enablement record boundaries in `feature_lifecycle.py` and
  `feature_plan.py` to the new codecs, delegated the existing removal-write block
  to `record_removed`, and added `restored_copies` to rollback plans. Keep those
  changes when merging A1's lifecycle work. The `release.py` codec import also
  changes so status can read numbered maintenance holds.
- A1 still owns migrating admission selection/subset checks, retained invocation
  records, listener transition fingerprints and open-set selections. In
  `admission.py`, resolution.roles is role policy: compare selected identities
  against deployment units, not strings against resolution.roles or copy 1 only.
  In `listeners.py`, separate concrete-copy record keys from role-level edge APIs.
- Lane C's archived `launch.secret_paths` must contain one entry per placed custom
  copy, keyed by the same `role` / `role.N` codec. A2 validates that exact coverage.
  The nested secret-name-to-path shape stays unchanged. Coordinate C's effective
  environment lookup signature with `feature_recovery._ready`, which still calls
  the base role-scoped API until C publishes its per-copy interface.
- At merge, call
  `acceptance_copies_ops.assert_copies_operations(host, project, profile, role="relay", backup_id=None)`
  after activating a disposable two-copy fixture whose selected role has
  `maintenance: stop` and no enabled dependents. Supply a completed backup ID from
  that placement to include the PITR drill and per-copy state sentinel assertions.
  The function leaves the selected role enabled and running. A2 did not edit
  `acceptance_feature.py` or execute containers.
- A2's two new runtime modules are included by the existing bundle inventory.
  Its exact-inventory unit now lists both alongside the existing optional socket
  probe helper. A1 owns deciding generation-17 required-file enforcement when
  finalizing the count-capable bundle.

### A2 files and verification

- Selectors: `local/cli.py`, `local/Makefile.example`, `server/manage.py`,
  `lib/journal.py`, `lib/operation_selection.py`.
- Records, rollback and holds: `lib/contracts.py`, `lib/copy_enablement.py`,
  `lib/feature_lifecycle.py`, `lib/feature_plan.py`, `lib/feature_maintenance.py`,
  `lib/release.py`. A rollback with missing evidence remains degraded even when
  maintenance holds otherwise cover every required copy.
- Reports and recovery: `lib/report.py`, `lib/backup.py`, `lib/recover.py`,
  `lib/feature_recovery.py`, `lib/recovery_rehearsal.py`, `local/rehearsal_host.py`.
- Tests: `tests/test_copies_ops.py`, `tests/acceptance_copies_ops.py`,
  `tests/test_acceptance_copies_ops.py`, `tests/test_backup.py`,
  `tests/test_upgrades.py`.
- Documentation: this handoff, `docs/guides/host-capabilities.md`, and
  `CHANGELOG.md` (the last two paths are repository-relative).

Verification was segmented: `make deploy-tools-check` ran 1,258 tests with 15
skips. Two new-module integration failures were corrected; all 44
`test_acceptance_*` units and the exact package-inventory test then passed.
Five temporary Unix-socket tests blocked by the sandbox passed with the required
permission. Copy/report/package checks passed in a 67-test rerun. Final
local/report/held-rollback checks passed in a 131-test rerun, and the expanded
copy operations checks passed separately. No container scenario was executed.
