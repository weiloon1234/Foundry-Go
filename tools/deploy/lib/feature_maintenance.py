"""Persistent per-copy holds; application pause and systemd enablement are distinct."""
import os

import admission
import services
from contracts import unit_selection_names, unit_selection_identities
from contracts import HOST_PATHS, atomic_json, atomic_write, record_document, require
from topology import EndpointBinding


def marker_path(unit):
    leaf = unit.role + "-1" if unit.ordinal == 1 else unit.record_key
    return f"{HOST_PATHS['state']}/admission/{unit.instance_id}/maintenance/{leaf}.hold"


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


def active_holds(host, entry, inventory):
    saved = record(host, entry)
    held = set(unit_selection_identities(inventory.profile.instance_id, saved["holds"])) if saved else set()
    held.update(unit_selection_identities(inventory.profile.instance_id,
                entry.get("deployment_selection", {}).get("maintenance_holds", ())))
    held.update(name for name, spec in inventory.specs.items()
                if admission.protected_path(host, host.paths.p(marker_path(spec.identity)), "file").exists())
    return held


def live(host, entry):
    import maintenance
    import release
    version = release.current_version(host, entry)
    return maintenance.status(host, entry, release.release_path(host, entry, version)) if version else None


def candidate(host, entry, state, request):
    import feature_lifecycle as lifecycle
    import release
    _, _, previous, launch = lifecycle.load(host, entry)
    stopped = set(previous.select(roles=tuple(stopped_roles(previous.topology) & previous.roles.keys())))
    saved = record(host, entry)
    affected = stopped | (set(unit_selection_identities(previous.profile.instance_id, saved["holds"])) & previous.units.keys() if saved else set())
    if not affected:
        return previous, launch, affected
    declaration = release.runtime_declaration(launch["declaration"])
    inventory, paths = lifecycle.resolve(host, entry, state["current"], declaration,
        release.release_path(host, entry, previous.deployment.release), launch["artifact_roots"], request["operation_id"],
        previous=previous, affected=affected, profile=previous.profile.projection(), preserve_private=True)
    launch = lifecycle.save_launch(host, entry, inventory, declaration, launch["artifact_roots"], paths)
    return inventory, launch, affected


def selection(host, entry, inventory, enabled, running, action, *, allow_start=True):
    """Compute intent without changing shared pause, markers, or processes."""
    saved = record(host, entry)
    observed = live(host, entry)
    prior_holds = active_holds(host, entry, inventory)
    if action in ("down", "up"):
        down = action == "down"
    else:
        down = observed["down"] if observed is not None else bool(prior_holds)
    holds = set(inventory.select(roles=tuple(stopped_roles(inventory.topology) & inventory.roles.keys()))) if down else set()
    if not allow_start or observed is None and action not in ("down", "up"):
        # Preserve even removed copies in the private journal until shared state
        # is observed. The selected graph carries only its declared holds.
        holds |= prior_holds
    prior_running = set(unit_selection_identities(inventory.profile.instance_id, saved["running"])) if saved and saved["holds"] else set(running)
    if down and saved and saved["holds"]:
        prior_running |= set(enabled) - set(unit_selection_identities(inventory.profile.instance_id, saved["enabled"]))
    if action == "up":
        # A deliberate disable always wins over the remembered pre-down selection.
        running = (set(running) | prior_running) & set(enabled) & inventory.units.keys()
    elif observed is not None or action == "down":
        running = set(running) | ((prior_holds - holds) & prior_running & set(enabled) & inventory.units.keys())
    running = set(running) - holds
    while any(not inventory.prerequisites({name}) <= running for name in running):
        running = {name for name in running if inventory.prerequisites({name}) <= running}
    holds |= (prior_holds & set(enabled) & inventory.units.keys()) - running
    value = {"operation_id": None, "holds": sorted(unit_selection_names(holds)), "enabled": sorted(unit_selection_names(enabled)),
             "running": sorted(unit_selection_names(prior_running | (set(running) if down else set()))),
             "restore_holds": sorted(unit_selection_names(prior_holds - holds))}
    return holds & inventory.units.keys(), running, value


def restore_failed_holds(host, entry, inventory, previous, operation):
    """Independent cleanup retains released holds if startup never completed."""
    from dataclasses import replace
    import operations
    path = operations.directory(host, operation) / "maintenance-intent.json"
    if not path.exists():
        return False
    intent = admission.read_owned(host, path, lambda value: value)
    released = set(unit_selection_identities(inventory.profile.instance_id, intent.get("restore_holds", ())))
    if not released:
        return False
    holds = set(unit_selection_identities(inventory.profile.instance_id, intent["holds"])) | released
    publish_markers(host, previous, holds)
    publish_markers(host, inventory, holds)
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    atomic_json(store.durable / "maintenance.json", {**intent, "holds": sorted(unit_selection_names(holds))}, mode=0o600)
    selected, resolution = store._selection()
    selected = replace(selected, maintenance_holds=tuple(sorted(unit_selection_names(holds & inventory.units.keys()))))
    store._write(store.durable / "selection.json", selected)
    entry["deployment_selection"] = record_document(selected)
    return True


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


def reconcile(host, project, profile, *, allow_start=True):
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
        if not allow_start and not observed["down"]:
            return  # A drill/inspection/stop cannot restore workloads as a side effect.
        saved = record(host, entry)
        _, _, inventory, _ = lifecycle.load(host, entry)
        expected = set(inventory.select(roles=tuple(stopped_roles(inventory.topology) & inventory.roles.keys()))) if observed["down"] else set()
        actual = set(unit_selection_identities(inventory.profile.instance_id, saved["holds"])) & inventory.units.keys() if saved else set()
        if not allow_start:
            expected |= actual
        if actual == expected:
            return
    lifecycle.request_operation(host, "down" if observed["down"] else "up", project, profile,
        maintenance_reconcile=True, **({"maintenance_no_start": True} if not allow_start else {}))
