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
