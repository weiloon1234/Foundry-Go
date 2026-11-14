"""Pinned feature-unit phases. Only runtime-v1 calls this installed module."""
import os
from pathlib import Path
import pwd
import time

import admission
from contracts import (Deployment, HelperInvocation, RuntimeResolution, canonical_digest,
                       check_path, digest, read_json, record_document, require)
from host import Host
import runtime_config
import services
from topology import ExecutableRole, parse_profile, parse_topology


def selected(host, pin, launch):
    invocation = HelperInvocation.parse(pin)
    deployment = Deployment.parse(launch["deployment"])
    resolution = RuntimeResolution.parse(launch["resolution"])
    topology = parse_topology(launch["topology"])
    profile = parse_profile(launch["profile"], topology)
    registry = host.read_registry()
    entry = registry["instances"][invocation.unit.instance_id]
    inventory = services.UnitInventory(entry["name"], topology, profile, deployment, resolution)
    spec = inventory.specs[invocation.unit.role]
    require(canonical_digest(record_document(spec)) == invocation.unit_spec_sha256 and
            resolution.id == invocation.resolution_id and entry["profile"] == profile.projection(),
            "feature_runtime_selection", "Pinned unit differs from its immutable launch record.")
    return invocation, entry, inventory, spec


def run(action, pin, launch):
    host = Host()
    invocation, entry, inventory, spec = selected(host, pin, launch)
    role = inventory.roles[spec.identity.role]
    paths = services.ServicePaths.for_unit(spec.identity, invocation.resolution_id)
    view = host.paths.p(paths.base) / "invocations" / invocation.process.invocation_id
    dispatch = view / "dispatch.json"
    expected_unit = f"foundry-admission-{spec.identity.instance_id}.service" if action == "boot" else inventory.units[spec.identity.role]
    if action == "open":
        expected_unit = f"foundry-admission-open-{spec.identity.instance_id}.service"
    groups = host.paths.p("/proc/self/cgroup").read_text().splitlines()
    require(any(line.split(":", 2)[-1].endswith("/" + expected_unit) for line in groups),
            "feature_runtime_identity", "Runtime helper is outside the declared systemd unit.")
    if action == "dispatch":
        require(os.geteuid() != 0 and pwd.getpwuid(os.geteuid()).pw_name == entry["name"],
                "feature_runtime_account", "Workload dispatch requires the instance account.")
        prepared = read_json(check_path(dispatch, "file"))
        require(prepared["invocation"] == record_document(invocation) and prepared["argv"] == [spec.executable, *spec.arguments],
                "feature_runtime_preparation", "Workload has no matching prepared invocation.")
        require(digest(check_path(host.paths.p(spec.executable), "file")) == spec.executable_sha256,
                "service_executable", "Selected executable fingerprint has drifted.")
        os.chdir(entry["allocations"]["paths"]["data"])
        os.execve(spec.executable, prepared["argv"], prepared["environment"])
    require(os.geteuid() == 0, "root_required", "Owned runtime preparation requires root.")
    if action == "cleanup":
        # Preview deliberately retains invocation evidence and all referenced bundles.
        return 0
    if action in ("boot", "open"):
        from feature_lifecycle import boot, open_boot, load
        store, _, current, _ = load(host, entry)
        require(current.specs[spec.identity.role] == spec and
                (invocation.resolution_id == current.resolution.id or
                 store.retained_origin(invocation, current.resolution) is not None),
                "feature_runtime_selection", "Coordinator generation differs from the selected role origin.")
        (boot if action == "boot" else open_boot)(host, entry, current)
        return 0
    import recovery_rehearsal
    if recovery_rehearsal.active(entry) and isinstance(role, ExecutableRole):
        require(role.recovery_rehearsal_safe, "recovery_dependency",
                f"Unsafe role {spec.identity.role} cannot run in a recovery rehearsal.")
        recovery_rehearsal.isolation(host, inventory, spec.identity.role)
    store = admission.AdmissionStore(host, spec.identity.instance_id)
    selection, current_resolution = store._selection()
    permit_id = None
    if selection.admission.disposition.value == "pending" and store.retained_invocation(invocation, current_resolution) is None:
        permit_path = admission.protected_path(host, store.runtime / "active-permit", "file")
        require(permit_path.exists(), "admission_denied", "Pending starts require an explicit validation allowance.")
        permit_id = permit_path.read_text()
    if action == "admit":
        store.admit(invocation, permit_id=permit_id)
        return 0
    if action == "running":
        from contracts import ProcessIdentity
        from release import service, _matches_executable
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            observed = service(host, inventory.units[spec.identity.role], timeout=min(5, deadline - time.monotonic()))
            if (observed["ActiveState"] in ("active", "activating") and
                    _matches_executable({**observed, "ActiveState": "active"}, host.paths.p(spec.executable))):
                process = ProcessIdentity(int(observed["MainPID"]), int(observed["process"]["start"]),
                                          invocation.process.invocation_id)
                require(admission.executor_alive(host, process), "feature_runtime_identity", "Workload invocation changed.")
                store.bind_workload(invocation, process, permit_id=permit_id)
                return 0
            time.sleep(.05)
        require(False, "feature_runtime_identity", "Selected workload did not acquire its systemd MainPID.")
    require(action == "prepare", "feature_runtime_action", "Unsupported runtime action.")
    roots = {key: tuple(value) for key, value in launch["artifact_roots"].items()}
    from contracts import ReleaseDeclaration
    from release import runtime_declaration
    declaration = runtime_declaration(launch["declaration"])
    views = services.bundle_views(role, paths, topology=inventory.topology,
                                  architecture=inventory.profile.architecture, artifact_roots=roots, payloads=declaration.artifacts)
    release = host.paths.opt(entry["name"]) / "releases" / inventory.deployment.release
    if isinstance(role, ExecutableRole):
        for path in launch["secret_paths"].get(spec.identity.role, {}).values():
            runtime_config._read_value_file(host, entry, path)
        runtime_config.prepare_service(host, store, invocation, role, spec, release, declaration.inventory,
                                       views=views, secret_paths=launch["secret_paths"].get(spec.identity.role, {}),
                                       uid=entry["allocations"]["uid"], gid=entry["allocations"]["gid"], permit_id=permit_id)
    with store.preparation(invocation, permit_id=permit_id):
        runtime_config.effective_environment_path(host, entry, inventory.resolution, spec.identity.role)
        admission.protected_directory(host, view)
        require(digest(check_path(host.paths.p(spec.executable), "file")) == spec.executable_sha256,
                "service_executable", "Selected executable fingerprint has drifted.")
        environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
                       "INVOCATION_ID": invocation.process.invocation_id,
                       "WEB__PUBLIC_DIR": str(release / "public"),
                       "FOUNDRY_DEPLOY_RUNTIME_FILE": launch["descriptors"][spec.identity.role]}
        if isinstance(role, ExecutableRole):
            secrets = launch["secret_paths"].get(spec.identity.role, {})
            for key, value in role.environment:
                require(key not in environment and not key.startswith("FOUNDRY_"),
                        "service_environment", "Custom environment cannot override framework runtime identity.")
                environment[key] = services.resolve_argument(value, role, spec, inventory.resolution, paths, views, secrets)
            environment.update({key: secrets[key] for key, _ in role.secret_files})
        from contracts import atomic_json
        atomic_json(dispatch, {"invocation": record_document(invocation), "argv": [spec.executable, *spec.arguments],
                               "environment": environment}, mode=0o644)
    return 0
