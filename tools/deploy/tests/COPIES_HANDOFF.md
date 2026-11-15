# Numbered service copies: shared interface

Release 2a, lane A1, turn 1 supplies the foundation. Counts above one still fail
with `unsupported_capability` in `topology.validate_feature_profile`. A1 removes
that gate only after migrating lifecycle callers together in turn 2. Wrappers
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

Turn 1 keeps `UnitInventory`'s existing role-keyed callers behind the singleton
gate. Turn 2 replaces their keys with `UnitIdentity`; it must not add an extras
map or retain a second runtime inventory. Role declarations remain role policy.
Dependency edges expand to every prerequisite copy. Every copy must pass its own
readiness and stability checks under one shared deadline.

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

Arguments and environment accept `{"copy_ordinal": "self"}`, resolved as the
decimal ordinal. The existing `copy_number` spelling remains supported. Rendered
JSON field bindings resolve either spelling to an integer.

`FEATURE_COPIES_GENERATION = 17` is the client and host floor for counts above one
or the new `copy_ordinal` spelling. Older hosts can still use `copy_number`.
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
