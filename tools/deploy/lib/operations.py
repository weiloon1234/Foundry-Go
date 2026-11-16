"""Pinned operation execution and independent systemd ExecStopPost cleanup."""
import hashlib
import os
from pathlib import Path
import re
import shutil

import admission
from contracts import (AdmissionDisposition, Deployment, DeployError, HostTransition, RuntimeResolution,
                       atomic_json, canonical_digest, contract_uuid, record_document, require)
from host import Host
import services
import operation_lock
from topology import parse_profile, parse_topology


def directory(host, operation):
    return host.paths.state / "feature-operations" / contract_uuid(operation)


def staging(host, entry, operation):
    # Same filesystem as releases for atomic publication; never in release inventory.
    return host.paths.opt(entry["name"]) / "operations" / contract_uuid(operation)


# The only approved runtime-v1 bridge: v0.35.0 to numbered copies. Both
# revisions retain format-1 singleton, operation and network-boot invocation pins.
RUNTIME_V16_SHA256 = "52b7afcc99ea4237fd80b7071d39d5f210c44518f204d324b271364fd2805295"
RUNTIME_COPIES_SHA256 = "d74a742b08ab6006fdeb58d4908227b428285cfbe251c74d106e08a4e26fbf15"


def runtime_compatible(installed, expected):
    return installed == expected or (
        hashlib.sha256(installed).hexdigest(), hashlib.sha256(expected).hexdigest()
    ) == (RUNTIME_COPIES_SHA256, RUNTIME_V16_SHA256)


def runtime_migration(host, tools_dir):
    destination = admission.protected_path(host, host.paths.p(services.RUNTIME_ENTRY), "file")
    if not destination.exists():
        return None
    content = (Path(tools_dir) / "server/runtime_v1.py").read_bytes()
    installed = destination.read_bytes()
    if installed == content:
        return None
    before, after = hashlib.sha256(installed).hexdigest(), hashlib.sha256(content).hexdigest()
    require((before, after) == (RUNTIME_V16_SHA256, RUNTIME_COPIES_SHA256),
            "runtime_bridge_required", "Installed runtime-v1 has no approved bridge to the candidate helper.")
    return {"path": services.RUNTIME_ENTRY, "from_sha256": before, "to_sha256": after}


def migrate_runtime(host, tools_dir):
    """Publish only the approved bridge, under the tools upgrade's durable intent."""
    require(host.host_lock_fd is not None, "tools_lock_required", "Runtime migration requires the host lock.")
    if runtime_migration(host, tools_dir):
        host.owned_write(host.paths.p(services.RUNTIME_ENTRY),
                         (Path(tools_dir) / "server/runtime_v1.py").read_bytes(), mode=0o755)


def check_runtime(host, tools_dir):
    destination = host.paths.p(services.RUNTIME_ENTRY)
    source = Path(tools_dir) / "server/runtime_v1.py"
    admission.protected_path(host, destination, "file")
    if destination.exists():
        require(destination.read_bytes() == source.read_bytes(),
                "runtime_bridge_required", "Replacing runtime-v1 requires an explicit bridge release.")
    return destination, source.read_bytes()


def install_runtime(host, tools_dir):
    destination, content = check_runtime(host, tools_dir)
    host.owned_write(destination, content, mode=0o755)


def command(action, operation, bundle):
    return ["/usr/bin/python3", "-I", services.RUNTIME_ENTRY, action, operation, bundle]


def prepare(host, registry, entry, request):
    import upgrades
    bundle = host.paths.p(registry["tools"]["active_path"])
    require((bundle / "lib/operations.py").is_file(), "capability_unavailable",
            "Feature activation requires the generation-12 independent operation cleanup release.")
    upgrades._installed(host, registry["tools"])
    install_runtime(host, bundle)
    root = directory(host, request["operation_id"])
    admission.protected_directory(host, root, 0o700)
    owner = {"schema": 1, "operation_id": request["operation_id"], "instance_id": entry["profile"]["instance_id"],
             "bundle_sha256": registry["tools"]["bundle_sha256"], "request_sha256": canonical_digest(request)}
    atomic_json(root / "owner.json", owner, mode=0o600)
    atomic_json(root / "request.json", request, mode=0o600)
    admission.AdmissionStore(host, owner["instance_id"]).cleanup_status(owner["operation_id"], "0" * 32, "reserved")
    return owner


def verify_executor(host, request, pin):
    require(isinstance(pin, dict) and pin["operation_id"] == request["operation_id"] and
            pin["instance_id"] == request["profile"]["instance_id"] and
            pin["request_sha256"] == canonical_digest(request) and pin["boot_id"] == admission.boot_id(host) and
            pin["invocation_id"] == os.environ.get("INVOCATION_ID"),
            "capability_unavailable", "Feature activation requires its invocation-pinned independent cleanup owner.")
    unit = "foundry-operation-" + request["operation_id"] + ".service"
    observed = host.run(["systemctl", "show", unit, "--property=ExecStart,ExecStopPost,Type,KillMode,Restart,Transient,DropInPaths", "--no-pager"])
    values = dict(line.split("=", 1) for line in observed.stdout.splitlines() if "=" in line)
    for directive, action in (("ExecStart", "operation"), ("ExecStopPost", "operation-cleanup")):
        argv = re.findall(r"argv\[\]=(.*?) ;", values.get(directive, ""))
        require(argv == [" ".join(command(action, pin["operation_id"], pin["bundle_sha256"]))],
                "capability_unavailable", "The loaded operation unit lacks its exact pinned cleanup owner.")
    require(all(values.get(k) == v for k, v in {"Type": "exec", "KillMode": "control-group", "Restart": "no",
            "Transient": "yes", "DropInPaths": ""}.items()), "operation_owner", "Operation unit policy differs from its installed owner.")


def bind_transition(host, entry, inventory, transition):
    value = {"schema": 1, "transition": record_document(transition), "deployment": record_document(inventory.deployment),
             "resolution": record_document(inventory.resolution), "topology": inventory.topology.projection(),
             "profile": inventory.profile.projection()}
    path = directory(host, transition.operation_id) / "transition.json"
    admission.protected_directory(host, path.parent, 0o700)
    if path.exists():
        require(admission.read_owned(host, path, lambda v: v) == value,
                "operation_owner", "Immutable cleanup scope differs from this transition.")
    else:
        atomic_json(path, value, mode=0o600)


def completion(host, entry, pin):
    """Require all durable completion owners, not just a success return value."""
    import feature_lifecycle
    root = directory(host, pin["operation_id"])
    if not (root / "complete.json").exists():
        return False
    receipt = admission.read_owned(host, root / "complete.json", lambda v: v)
    result = admission.read_owned(host, root / "result.json", lambda v: v)
    store, selected, _, _ = feature_lifecycle.load(host, entry)
    state = host.load_state(entry)
    attempt = state.get("last_attempt") or {}
    no_graph_change = (attempt.get("maintenance_action") in ("down", "up")
                       and not (root / "transition.json").exists())
    return (receipt == {"schema": 1, "operation_id": pin["operation_id"], "invocation_id": pin["invocation_id"],
                        "result_sha256": canonical_digest(result)} and result.get("ok") is True and
            selected.admission.disposition is AdmissionDisposition.COMMITTED and
            (selected.admission.operation_id == pin["operation_id"] or no_graph_change) and
            entry.get("deployment_selection") == record_document(selected) and
            attempt.get("operation_id") == pin["operation_id"] and attempt.get("outcome") == "succeeded" and
            store._operations()[pin["operation_id"]]["outcome"] == "succeeded")


def clean_staging(host, entry, operation):
    path = admission.protected_path(host, staging(host, entry, operation), "directory")
    if path.exists():
        shutil.rmtree(path)
        fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def cleanup_reconciled(attempt):
    return (attempt.get("outcome") == "failed"
            and attempt.get("error") == "operation_cleanup_reconciled")


def reconcile_closed(host, entry):
    """Release only a proven closed health failure, under host and instance locks."""
    import feature_lifecycle
    import feature_network
    import release
    from contracts import ClosureEvidence, ClosureStage, unit_selection_identities
    state = host.load_state(entry)
    attempt = state.get("last_attempt") or {}
    recovered = cleanup_reconciled(attempt)
    require((recovered or attempt.get("outcome") == "needs_reconciliation"
             and attempt.get("error") == "operation_cleanup_pending")
            and attempt.get("phase") == "health" and attempt.get("action") in ("deploy", "rollback", "restart")
            and "maintenance_action" not in attempt and "recovery_mode" not in attempt,
            "operation_reconcile_unavailable", "Only closed feature health failures can be reconciled to stopped; inspect status/report.")
    operation = contract_uuid(attempt.get("operation_id"))
    root = directory(host, operation)
    store, selected, inventory, _ = feature_lifecycle.load(host, entry)
    cleanup = store.cleanup_records()
    owner = cleanup.get(operation)
    require(owner is not None and (owner["status"] == "closed" or recovered and owner["status"] == "complete")
            and all(value["status"] == "complete" for key, value in cleanup.items() if key != operation),
            "operation_cleanup_pending", "Cleanup must be closed, with no other unresolved cleanup owner.")
    attempts = store._operations()
    recorded = attempts.get(operation, {})
    require(all(recorded.get(key) == attempt.get(key) for key in
                ("id", "operation_id", "action", "release", "phase", "running_roles"))
            and (cleanup_reconciled(recorded) or (recorded.get("outcome"), recorded.get("error")) ==
                 ("needs_reconciliation", "operation_cleanup_pending"))
            and all(value["outcome"] in admission.TERMINAL_OUTCOMES for key, value in attempts.items() if key != operation),
            "operation_cleanup_evidence", "Operation journals do not identify one closed health failure.")
    import dns
    require(not any({"pending", "conflict"}.intersection(item["results"].values())
                    or item.get("public_admission") == dns.PublicAdmission.PENDING.value
                    for item in dns.load(host, entry)["operations"].values()),
            "dns_reconciliation", "Resolve uncertain DNS writes or public admission before cleanup reconciliation.")
    value = admission.read_owned(host, root / "transition.json", lambda v: v)
    transition = HostTransition.parse(value["transition"])
    evidence = admission.read_owned(host, root / "cleanup-closure.json", ClosureEvidence.parse)
    old = feature_lifecycle.load_resolution(host, entry, transition.old_resolution_id)[0]
    units = {**old.units, **inventory.units}
    require(selected.admission.disposition is AdmissionDisposition.PENDING
            and selected.admission.operation_id == operation
            and entry.get("deployment_selection") == record_document(selected)
            and transition.operation_id == operation and transition.instance_id == store.instance_id
            and transition.candidate_resolution_id == inventory.resolution.id
            and value["deployment"] == record_document(inventory.deployment)
            and value["resolution"] == record_document(inventory.resolution)
            and value["topology"] == inventory.topology.projection() and value["profile"] == inventory.profile.projection()
            and set(transition.affected_units) == set(units)
            and evidence.stage is ClosureStage.APPLICATION_CLOSED and evidence.operation_id == operation
            and evidence.old_resolution_id == transition.old_resolution_id
            and evidence.candidate_resolution_id == transition.candidate_resolution_id
            and set(evidence.units) == set(units) and set(inventory.resolution.endpoints) <= set(evidence.endpoints)
            and attempt["release"] == inventory.deployment.release == release.current_version(host, entry)
            and (state.get("current") or {}).get("version") == attempt["release"]
            and inventory.resolution.private_revision == entry["configuration_revision"]
            and "running_roles" in attempt and (not recovered or state["status"] == "stopped")
            and set(unit_selection_identities(store.instance_id, attempt["running_roles"])) <= inventory.units.keys(),
            "operation_cleanup_evidence", "Reconciliation requires matching full-instance selection, release and complete closure evidence.")
    # Include the independent cleanup process and queued jobs, not just MainPIDs.
    services.stopped_unit(host, "foundry-operation-" + operation + ".service")
    for unit in units.values():
        services.stopped_unit(host, unit)
    # Reobserve through the network owner; old receipt hashes alone cannot prove
    # that no socket/route has appeared since cleanup (or across a reboot).
    closure = feature_network.close(host, entry, transition, inventory.resolution, units)
    store.record_boot_closure(closure)
    active = admission.protected_path(host, store.runtime / "active-permit", "file")
    if active.exists():
        store.revoke_permit(contract_uuid(active.read_text()))
        active.unlink()
    unchanged = recovered and owner["status"] == "complete"
    state["status"] = "stopped"
    if not recovered:
        release.finish(host, entry, state, "failed", "operation_cleanup_reconciled")
    store.record_operation(state["last_attempt"])
    # Admission stays pending: completion is closure, never deployment success.
    # Last write releases the blocker; every earlier crash is safely repeatable.
    store.cleanup_status(operation, owner["invocation_id"], "complete")
    return {"ok": True, "status": "stopped", "cleanup_reconciled": True, "unchanged": unchanged,
            "operation_id": operation, "version": inventory.deployment.release}


def cleanup(host, request, pin):
    import feature_lifecycle
    import feature_network
    import release
    operation = pin["operation_id"]
    with operation_lock.acquire(host, request, cleanup=True), host.lock(pin["instance_id"]):
        registry = host.read_registry()
        entry = registry["instances"][pin["instance_id"]]
        require(release.legacy_profile(entry["profile"]) == release.legacy_profile(request["profile"]),
                "operation_identity", "Operation infrastructure identity changed.")
        registry, entry = host.require_identity(request["project"], entry["profile"])
        store = admission.AdmissionStore(host, pin["instance_id"])
        try:
            complete = completion(host, entry, pin)
        except (DeployError, OSError, KeyError):
            complete = False
        if complete:
            clean_staging(host, entry, operation)
            store.cleanup_status(operation, pin["invocation_id"], "complete")
            return 0
        # This survives another cleanup crash and denies validation as well as
        # ordinary starts, even if the executor had just committed admission.
        store.cleanup_status(operation, pin["invocation_id"], "pending")
        state = host.load_state(entry)
        attempt = state.get("last_attempt") or {}
        applied = attempt.get("operation_id") == operation and (
            attempt.get("outcome") == "failed_applied" or
            attempt.get("outcome") == "failed" and attempt.get("maintenance_action") in ("down", "up"))
        context = directory(host, operation) / "transition.json"
        if not context.exists() and (attempt.get("operation_id") != operation or attempt.get("outcome") == "failed"):
            store.cleanup_status(operation, pin["invocation_id"], "complete")
            return 0
        if attempt.get("operation_id") == operation and not applied:
            state["status"] = "unknown"
            release.finish(host, entry, state, "needs_reconciliation", "operation_cleanup_pending")
            store.record_operation(state["last_attempt"])
        context = directory(host, operation) / "transition.json"
        if not context.exists():
            # Transition intent always precedes runtime effects. Keep staging and
            # the unresolved owner until an explicit reconciliation can release it.
            store.cleanup_status(operation, pin["invocation_id"], "closed")
            return 0
        value = admission.read_owned(host, context, lambda v: v)
        transition = HostTransition.parse(value["transition"])
        topology = parse_topology(value["topology"])
        inventory = services.UnitInventory(entry["name"], topology, parse_profile(value["profile"], topology),
                                           Deployment.parse(value["deployment"]), RuntimeResolution.parse(value["resolution"]))
        affected = set(transition.affected_units)
        require(transition.operation_id == operation and transition.instance_id == pin["instance_id"] and
                transition.candidate_resolution_id == inventory.resolution.id and
                all(unit.instance_id == pin["instance_id"] for unit in transition.affected_units),
                "operation_cleanup", "Cleanup workload scope differs from its pinned transition.")
        from feature_lifecycle import load_resolution
        old = inventory if transition.old_resolution_id == inventory.resolution.id else load_resolution(host, entry, transition.old_resolution_id)[0]
        import feature_maintenance
        if feature_maintenance.restore_failed_holds(host, entry, inventory, old, operation):
            host.journal(registry, entry)
        units = {**old.units, **inventory.units}
        require(affected <= units.keys(), "operation_cleanup", "Unknown affected cleanup unit.")
        try:
            feature_network.withdraw(host, entry, transition, inventory.resolution, units)
        except Exception:
            pass  # Still stop every workload; only the final close can prove closure.
        stopped = True
        order = tuple(dict.fromkeys((*inventory.stop_order(affected & inventory.units.keys()),
                                    *old.stop_order(affected & old.units.keys()))))
        for name in order:
            try:
                feature_lifecycle.stop_units(host, inventory if name in inventory.units else old, (name,))
            except Exception:
                stopped = False
        try:
            evidence = feature_network.close(host, entry, transition, inventory.resolution, units)
            closed = True
            atomic_json(directory(host, operation) / "cleanup-closure.json", record_document(evidence), mode=0o600)
        except Exception:
            closed = False
        if stopped and closed:
            store.record_boot_closure(evidence)
            root = directory(host, operation)
            initial = root / "initial-validation.json"
            reconciled = applied
            import dns
            if (attempt.get("operation_id") == operation and request["action"] in ("activate", "rollback")
                    and dns.rejected_operation(host, entry, operation)):
                state["status"] = "failed"
                release.finish(host, entry, state, "failed", "dns_provider_rejected")
                store.record_operation(state["last_attempt"])
                entry["deployment_selection"] = record_document(store._selection()[0])
                host.journal(registry, entry)
                reconciled = True
            if (initial.exists() and not (root / "validated.json").exists()
                    and "deployment_selection" not in entry and release.current_version(host, entry) is None):
                before = admission.read_owned(host, initial, lambda v: v)
                require(before["current"] is None and before["configuration_revision"] == entry["configuration_revision"],
                        "operation_cleanup", "Initial validation changed persistent configuration.")
                state.update({key: before[key] for key in ("status", "current", "previous")})
                release.finish(host, entry, state, "failed", "feature_validation_failed")
                store.record_operation(state["last_attempt"])
                reconciled = True
            store.cleanup_status(operation, pin["invocation_id"], "complete" if reconciled else "closed")
    # Closed workloads do not reconcile an interrupted deploy. An explicit
    # reconcile can release a proven closed health failure without starting it.
        return 0 if stopped and closed else 1


def run(action, pin):
    import feature_lifecycle
    host = Host()
    root = directory(host, pin["operation_id"])
    request = admission.read_owned(host, root / "request.json", lambda v: v)
    require(canonical_digest(request) == pin["request_sha256"], "operation_owner", "Pinned request has changed.")
    with host.command_journal(root / (action + "-commands.json")):
        if action == "operation-cleanup":
            return cleanup(host, request, pin)
        verify_executor(host, request, pin)
        store = admission.AdmissionStore(host, pin["instance_id"])
        store.require_cleanup_clear(pin["operation_id"])
        store.cleanup_status(pin["operation_id"], pin["invocation_id"], "armed")
        try:
            if request["action"] == "recover-activate":
                import feature_recovery
                result = feature_recovery.execute(host, request, pin)
            else:
                result = feature_lifecycle.execute(host, request, pin=pin)
        except (DeployError, OSError) as error:
            result = {"error": {"code": error.code if isinstance(error, DeployError) else "filesystem_failed",
                                "detail": error.detail if isinstance(error, DeployError) else "Inspect the owned operation journal."}}
        atomic_json(root / "result.json", result, mode=0o600)
        if result.get("ok") is True:
            atomic_json(root / "complete.json", {"schema": 1, "operation_id": pin["operation_id"],
                        "invocation_id": pin["invocation_id"], "result_sha256": canonical_digest(result)}, mode=0o600)
        return 0 if result.get("ok") is True else 1


def launch(host, request, owner):
    operation_id = request["operation_id"]
    root = directory(host, operation_id)
    store = admission.AdmissionStore(host, request["profile"]["instance_id"])
    unit = "foundry-operation-" + operation_id
    cleanup = services.command_line(command("operation-cleanup", operation_id, owner["bundle_sha256"]))
    from rehearsal_storage import environment as rehearsal_environment
    rehearsal = ["--setenv=" + key + "=" + value for key, value in rehearsal_environment({"profile": request["profile"]}).items()]
    result = host.run(["systemd-run", "--quiet", "--wait", "--collect", "--unit", unit,
              "--property=Type=exec", "--property=Restart=no", "--property=KillMode=control-group",
              "--property=TimeoutStopSec=3600", "--property=RuntimeMaxSec=1800", "--property=ExecStopPost=" + cleanup,
              *rehearsal, *command("operation", operation_id, owner["bundle_sha256"])], timeout=5400, check=False)
    receipt = admission.read_owned(host, root / "result.json", lambda value: value)
    if "error" in receipt:
        raise DeployError(receipt["error"]["code"], receipt["error"]["detail"])
    store.require_cleanup_clear()
    require(result.returncode == 0, "feature_operation_failed", "Operation exit differs from its receipt; inspect the journal.")
    return receipt
