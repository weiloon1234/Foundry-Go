"""Persistent per-copy holds; application pause and systemd enablement are distinct."""
import os

import admission
from contracts import HOST_PATHS, atomic_json, atomic_write, require
from topology import EndpointBinding


def marker_path(unit):
    return f"{HOST_PATHS['state']}/admission/{unit.instance_id}/maintenance/{unit.role}-{unit.ordinal}.hold"


def stopped_roles(topology):
    if topology.schema == 1:
        return set()  # Legacy workers/schedulers retain their application pause contract.
    roles = {role.common.name: role for role in topology.roles}
    stopped = {name for name, role in roles.items() if role.common.maintenance == "stop"}
    require("http" not in stopped, "maintenance_graph",
            'Role http must remain running; declare maintenance: "continue" for http.')
    for name, role in roles.items():
        if name in stopped:
            continue
        for binding_name, binding in role.common.bindings:
            if isinstance(binding, EndpointBinding):
                require(binding.role not in stopped, "maintenance_graph",
                        f'Role {name} continues but endpoint binding {binding_name} targets stopped role {binding.role}; '
                        f'declare {binding.role} maintenance: "continue", or remove binding {binding_name} from {name}.')
        for provider in role.common.requires:
            require(provider not in stopped, "maintenance_graph",
                    f'Role {name} continues but requires stopped role {provider}; '
                    f'declare {provider} maintenance: "continue", or remove that requires prerequisite from {name}.')
    return stopped


def record(host, entry):
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    path = store.durable / "maintenance.json"
    return admission.read_owned(host, path, lambda value: value) if path.exists() else None


def live(host, entry):
    import maintenance
    import release
    version = release.current_version(host, entry)
    return maintenance.status(host, entry, release.release_path(host, entry, version)) if version else None


def candidate(host, entry, state, request):
    import feature_lifecycle as lifecycle
    import release
    _, _, previous, launch = lifecycle.load(host, entry)
    stopped = stopped_roles(previous.topology) & previous.roles.keys()
    saved = record(host, entry)
    affected = stopped | (set(saved["holds"]) & previous.roles.keys() if saved else set())
    if not affected:
        return previous, launch, affected
    declaration = release.runtime_declaration(launch["declaration"])
    inventory, paths = lifecycle.resolve(host, entry, state["current"], declaration,
        release.release_path(host, entry, previous.deployment.release), launch["artifact_roots"], request["operation_id"],
        previous=previous, affected=affected, profile=previous.profile.projection())
    launch = lifecycle.save_launch(host, entry, inventory, declaration, launch["artifact_roots"], paths)
    return inventory, launch, affected


def selection(host, entry, inventory, enabled, running, action):
    """Compute intent without changing shared pause, markers, or processes."""
    saved = record(host, entry)
    observed = live(host, entry)
    if action in ("down", "up"):
        down = action == "down"
    else:
        down = (observed is not None and observed["down"]) or bool(saved and saved["holds"])
    holds = (stopped_roles(inventory.topology) & inventory.roles.keys()) if down else set()
    prior_running = set(saved["running"]) if saved and saved["holds"] else set(running)
    if down and saved and saved["holds"]:
        prior_running |= set(enabled) - set(saved["enabled"])
    if action == "up":
        # A deliberate disable always wins over the remembered pre-down selection.
        running = (set(running) | prior_running) & set(enabled) & inventory.roles.keys()
    running = set(running) - holds
    while any(not set(inventory.roles[name].common.requires) <= running for name in running):
        running = {name for name in running if set(inventory.roles[name].common.requires) <= running}
    value = {"operation_id": None, "holds": sorted(holds), "enabled": sorted(enabled),
             "running": sorted(prior_running | (set(running) if down else set()))}
    return holds, running, value


def publish_markers(host, inventory, holds):
    for name, spec in inventory.specs.items():
        if name in holds:
            path = host.paths.p(marker_path(spec.identity))
            admission.protected_directory(host, path.parent, 0o700)
            atomic_write(path, "maintenance\n", mode=0o600)


def clear_markers(host, inventory, holds):
    # Called only inside the owner, after pending admission and proven closure.
    for name, spec in inventory.specs.items():
        path = admission.protected_path(host, host.paths.p(marker_path(spec.identity)), "file")
        if name not in holds and path.exists():
            path.unlink()
            fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)


def shared_change(host, entry, request):
    import maintenance
    import release
    action = request["action"]
    if request.get("maintenance_reconcile"):
        observed = live(host, entry)
        require(observed is not None and observed["down"] == (action == "down"),
                "maintenance_changed", "The shared maintenance record changed; retry reconciliation.")
        return observed
    path = release.release_path(host, entry, release.current_version(host, entry))
    return maintenance.run(host, entry, path, action, maintenance.arguments(action, request.get("maintenance", {})))


def reconcile(host, project, profile):
    """Observe first; the owned executor rechecks before any marker or stop."""
    import feature_lifecycle as lifecycle
    with host.lock("host"), host.lock(profile["instance_id"]):
        _, entry = host.require_identity(project, profile)
        if "deployment_selection" not in entry:
            return
        store = admission.AdmissionStore(host, profile["instance_id"])
        if (store.durable / "configuration-hold.json").exists():
            return
        observed = live(host, entry)
        if observed is None:
            return  # Unavailable shared state never clears a hold.
        saved = record(host, entry)
        _, _, inventory, _ = lifecycle.load(host, entry)
        expected = stopped_roles(inventory.topology) & inventory.roles.keys() if observed["down"] else set()
        actual = set(saved["holds"]) if saved else set()
        if actual == expected:
            return
    lifecycle.request_operation(host, "down" if observed["down"] else "up", project, profile, maintenance_reconcile=True)
