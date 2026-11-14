"""Feature lifecycle: reviewed transitions, private configuration and scoped admission."""
from dataclasses import asdict, replace
import json
import os
from pathlib import Path
import time
import uuid

import admission
import artifacts
import feature_network
import feature_maintenance
import packages
import operations
import runtime_config
import services
from contracts import (AdmissionDisposition, AdmissionRecord, Deployment,
                       DeploymentSelection, DeployError, FEATURE_CAPABILITIES, FEATURE_LIFECYCLE_GENERATION, HostTransition,
                       ProcessIdentity, ReleaseDeclaration, ResolvedBundleBinding, ResolvedEndpointBinding,
                       ResolvedExecutableBinding, RoleBindings, RuntimeResolution, UnitIdentity, UnitSpec,
                       ValidationPermit, atomic_json, atomic_write, canonical_digest, check_path, contract_uuid,
                       digest, marker, read_json, record_document, require)
from topology import (ApplicationExecutable, ArtifactExecutable, BuiltinRole, BundleDirectoryBinding,
                      EndpointBinding, ExecutableBinding, ExecutableRole, PackageExecutable, parse_profile, parse_topology)

LATER = "the post-preview B24 lifecycle release"


def is_feature(entry):
    return entry.get("topology", {}).get("schema") == 2 or "deployment_selection" in entry


def refuse(action):
    raise DeployError("capability_unavailable", f"Feature {action} requires {LATER}.")


def check_operation(action):
    require(action in ("activate", "rollback", "restart", "stop", "config-set", "down", "up"), "capability_unavailable", f"Feature operation requires {LATER}.")


def absolute(host, path):
    return "/" + Path(path).relative_to(host.paths.root).as_posix()


def load(host, entry):
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    selection, resolution = store._selection()
    inventory, launch = load_resolution(host, entry, resolution.id)
    return store, selection, inventory, launch


def load_resolution(host, entry, identifier):
    path = host.paths.opt(entry["name"]) / "runtime" / contract_uuid(identifier) / "launch.json"
    launch = admission.read_owned(host, path, lambda value: value)
    topology = parse_topology(launch["topology"])
    inventory = services.UnitInventory(entry["name"], topology, parse_profile(launch["profile"], topology),
                                       Deployment.parse(launch["deployment"]), RuntimeResolution.parse(launch["resolution"]))
    return inventory, launch


def migrate_enablement(host, registry, entry):
    """Adopt running preview units once, under host then instance locks; never start one."""
    import release
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    path = store.durable / "enablement.json"
    if "deployment_selection" not in entry:
        return None
    if path.exists() and entry["deployment_selection"]["minimum_generation"] >= FEATURE_LIFECYCLE_GENERATION:
        return admission.read_owned(host, path, lambda value: value)
    store, selection, inventory, launch = load(host, entry)
    if selection.admission.disposition is not AdmissionDisposition.COMMITTED:
        return {"state": "pending", "detail": "Preview enablement migration awaits a committed deployment."}
    require(registry["tools"]["generation"] >= FEATURE_LIFECYCLE_GENERATION,
            "host_upgrade_required", "Enablement adoption requires deploy generation 13.")
    store.require_cleanup_clear()
    if selection.minimum_generation < FEATURE_LIFECYCLE_GENERATION:
        selection = replace(selection, minimum_generation=FEATURE_LIFECYCLE_GENERATION)
        store._write(store.durable / "selection.json", selection)
        entry["deployment_selection"] = record_document(selection)
        host.journal(registry, entry)
    if path.exists():
        return admission.read_owned(host, path, lambda value: value)
    enabled = set(services.enabled_units(host, inventory.units))
    running = []
    for name, unit in inventory.units.items():
        observed = release.service(host, unit)
        require(observed["ActiveState"] in ("active", "inactive", "failed"),
                "service_enablement", "Wait for stable workloads before preview enablement migration.")
        if observed["ActiveState"] == "active":
            require(release._matches_executable(observed, host.paths.p(inventory.specs[name].executable)),
                    "service_enablement", "Running preview unit differs from its selected executable.")
            running.append(name)
    intent = {"schema": 1, "authority": "systemd", "state": "prepared", "enabled": sorted(enabled),
              "adopt_running": sorted(set(running) - enabled), "starts": []}
    admission.protected_directory(host, path.parent, 0o700)
    atomic_json(path.with_name("enablement-intent.json"), intent, mode=0o600)
    install_units(host, registry, entry, inventory, launch, Path(__file__).resolve().parents[1])
    if intent["adopt_running"]:
        host.run(["systemctl", "enable", *(inventory.units[name] for name in intent["adopt_running"])])
    result = {**intent, "state": "complete", "enabled": list(services.enabled_units(host, inventory.units))}
    atomic_json(path, result, mode=0o600)
    return result


def network_report(host, entry, host_claims):
    from contracts import ClosureEvidence
    from report import declared_network
    if "deployment_selection" not in entry:
        topology = parse_topology(entry["topology"], registered=True)
        return declared_network(host, parse_profile(entry["profile"], topology), topology=topology,
                                entry=entry, host_claims=host_claims)
    store, selection, inventory, _ = load(host, entry)
    require(entry["deployment_selection"] == record_document(selection),
            "network_selection", "Registry and private network selection differ.")
    path = store.runtime / "boot.json"
    closure = admission.read_owned(host, path, ClosureEvidence.parse) if path.exists() else None
    return declared_network(host, inventory.profile, topology=inventory.topology, entry=entry,
                            resolution=inventory.resolution, closure=closure, host_claims=host_claims)


def selected_releases(host, entry, state):
    import release
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    path = store.durable / "selected-releases.json"
    recorded = admission.read_owned(host, path, lambda v: v) if path.exists() else []
    require(isinstance(recorded, list) and len(recorded) <= 4096 and all(release.valid_version(v) for v in recorded),
            "release_history", "Invalid retained selection history.")
    versions = set(recorded)
    for manifest in (state.get("current"), state.get("previous")):
        if manifest:
            versions.add(manifest["version"])
    attempts = list(state.get("history", []))
    if (store.durable / "operations").exists():
        attempts.extend(store._operations().values())
    versions.update(a["release"] for a in attempts if a.get("phase") in ("switch", "health") and a.get("release"))
    require(len(versions) <= 4096 and all(release.valid_version(v) for v in versions),
            "release_history", "Retained selection history exceeds its supported bounds.")
    return versions



def record_selection(host, entry, state, version):
    versions = selected_releases(host, entry, state) | {version}
    path = admission.AdmissionStore(host, entry["profile"]["instance_id"]).durable / "selected-releases.json"
    admission.protected_directory(host, path.parent, 0o700)
    atomic_json(path, sorted(versions), mode=0o600)


def transition_request(entry, inventory, operation, *, names=None, previous=None):
    old = entry.get("deployment_selection", {}).get("selected_resolution_id", inventory.resolution.id)
    specs = {**(previous.specs if previous else {}), **inventory.specs}
    return HostTransition(operation, inventory.profile.instance_id, old, inventory.resolution.id,
                          entry["configuration_revision"], tuple(s.identity for name, s in specs.items() if names is None or name in names),
                          canonical_digest(record_document(inventory.deployment)), None)


def executable(host, reference, declaration, release_path, roots):
    if isinstance(reference, PackageExecutable):
        evidence = packages.executable_evidence(host, reference)
        return evidence["path"], evidence["sha256"]
    if isinstance(reference, ArtifactExecutable):
        artifact = dict(parse_topology(declaration.topology_json).artifacts)[reference.artifact]
        path = host.paths.p(roots[reference.artifact][0]) / dict(artifact.executables)[reference.entry]
    else:
        require(isinstance(reference, ApplicationExecutable), "service_executable", "Unknown executable source.")
        path = release_path / dict(declaration.application_executables)[reference.application]
    path = admission.protected_path(host, path, "file")
    require(path.is_file() and path.stat().st_mode & 0o111, "service_executable", "Selected executable is unavailable.")
    return absolute(host, path), digest(path)


def resolve(host, entry, manifest, declaration, release_path, roots, operation_id, *,
            private_paths=None, values=None, revision=None, previous=None, affected=None, profile=None):
    topology = parse_topology(declaration.topology_json)
    profile = parse_profile(profile if profile is not None else declaration.profile_json, topology)
    affected = set(affected) if affected is not None else {role.common.name for role in topology.roles}
    if previous is not None:
        # Endpoint consumers have changed effective inputs when their provider moves.
        while True:
            consumers = {role.common.name for role in topology.roles for _, binding in role.common.bindings
                         if isinstance(binding, EndpointBinding) and binding.role in affected}
            if consumers <= affected:
                break
            affected.update(consumers)
    retained = set(previous.roles) - affected if previous else set()
    endpoints, nginx = feature_network.allocate(host, entry, topology, profile, operation_id,
        retained=tuple(e for e in previous.resolution.endpoints if e.role in retained) if previous else None)
    resolution_id = str(uuid.uuid4())
    roles = tuple(role.common.name for role in topology.roles if dict(profile.placement)[role.common.name])
    environments = runtime_config.effective_environments(host, entry, resolution_id,
                   roles, endpoints, values=values)
    # Resolution-independent placeholders are replaced before any record is published.
    seed = RuntimeResolution(resolution_id, "0" * 64, revision or entry["configuration_revision"], roles,
                             endpoints, environments, "0" * 64, "0" * 64, nginx)
    specs = []
    secrets = runtime_config.service_secret_paths(host, entry, topology, paths=private_paths, profile=profile)
    raw_roles = {role["name"]: role for role in topology.projection()["roles"]}
    for role in topology.roles:
        name = role.common.name
        if name not in roles:
            continue
        if name in retained:
            specs.append(previous.specs[name])
            continue
        unit = UnitIdentity(profile.instance_id, name, 1)
        paths = services.ServicePaths.for_unit(unit, resolution_id)
        views = services.bundle_views(role, paths, topology, profile.architecture, roots, declaration.artifacts)
        executable_bindings, endpoint_bindings, bundle_bindings = [], [], []
        for binding_name, binding in role.common.bindings:
            if isinstance(binding, ExecutableBinding):
                path, checksum = executable(host, binding.executable, declaration, release_path, roots)
                executable_bindings.append(ResolvedExecutableBinding(binding_name, path, checksum))
            elif isinstance(binding, EndpointBinding):
                endpoint = next((e for e in endpoints if (e.role, e.listener) == (binding.role, binding.listener)), None)
                require(endpoint is not None, "service_binding", "Declared endpoint is unallocated.")
                endpoint_bindings.append(ResolvedEndpointBinding(binding_name, endpoint, binding.scheme))
            else:
                require(isinstance(binding, BundleDirectoryBinding), "service_binding", "Unknown binding kind.")
                view = next(v for v in views if v.name == binding.mount)
                bundle_bindings.append(ResolvedBundleBinding(binding_name, view.destination, view.artifact_sha256))
        bindings = RoleBindings(name, tuple(executable_bindings), tuple(endpoint_bindings), tuple(bundle_bindings))
        if isinstance(role, BuiltinRole):
            path = release_path / "artisan"
            binary, checksum = absolute(host, path), digest(path)
            command = {"http": "serve", "worker": "queue:work", "scheduler": "schedule:work"}[name]
            environment = host.paths.instance_config(entry["name"]) / next(e.path for e in environments if e.role == name)
            args = [command]
            if name == "worker":
                import release
                args += ["--connection", release.worker_connection(host, entry, release_path, env_file=environment)]
            args += ["--config", absolute(host, release_path / "config/deploy.toml"), "--env-file",
                     absolute(host, environment)]
            config_sha = canonical_digest({"config": manifest["files"]["config/deploy.toml"], "revision": seed.private_revision,
                                           "environment_path": next(e.path for e in environments if e.role == name)})
        else:
            binary, checksum = executable(host, role.executable, declaration, release_path, roots)
            config_sha = services.configuration_digest(role, declaration.inventory, views, secrets[name])
            draft = UnitSpec(unit, binary, checksum, (), canonical_digest(raw_roles[name]), bindings, config_sha)
            args = [services.resolve_argument(a, role, draft, seed, paths, views, secrets[name]) for a in role.arguments]
        specs.append(UnitSpec(unit, binary, checksum, tuple(args), canonical_digest(raw_roles[name]), bindings, config_sha))
    deployment = Deployment(profile.instance_id, manifest["version"], manifest["schema"], topology.sha256,
                            canonical_digest(profile.projection()), tuple(specs))
    resolution = replace(seed, deployment_sha256=canonical_digest(record_document(deployment)),
                         bindings_sha256=canonical_digest([record_document(s.bindings) for s in specs]),
                         readiness_sha256=canonical_digest([raw_roles[name].get("readiness", name) for name in roles]))
    inventory = services.UnitInventory(entry["name"], topology, profile, deployment, resolution)
    return inventory, secrets


def save_launch(host, entry, inventory, declaration, roots, secret_paths):
    descriptors = {name: runtime_config.write_binding_descriptor(host, entry, inventory.deployment,
                   inventory.resolution, spec, candidate=True) for name, spec in inventory.specs.items()}
    launch = {"schema": 1, "topology": inventory.topology.projection(), "profile": inventory.profile.projection(),
              "deployment": record_document(inventory.deployment), "resolution": record_document(inventory.resolution),
              "declaration": declaration.projection(), "artifact_roots": roots, "secret_paths": secret_paths,
              "descriptors": descriptors}
    path = host.paths.opt(entry["name"]) / "runtime" / inventory.resolution.id / "launch.json"
    if path.exists():
        require(read_json(path) == launch, "runtime_resolution_conflict", "Immutable launch record already differs.")
    host.owned_write(path, json.dumps(launch, sort_keys=True) + "\n", mode=0o640, group=entry["name"])
    return launch


def install_units(host, registry, entry, inventory, launch, tools_dir):
    import provision
    import upgrades
    checksum = upgrades.tool_bundle(tools_dir)[2]
    require(registry["tools"]["bundle_sha256"] == checksum, "host_upgrade_required", "Install the selected preview tools first.")
    operations.install_runtime(host, tools_dir)
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    from release import runtime_declaration
    provision._unit_checks(host, entry)
    origins = store._retained_records(inventory.resolution)
    generations = {}
    for name, spec in inventory.specs.items():
        source, source_launch = inventory, launch
        if name in origins:
            source, source_launch = load_resolution(host, entry, origins[name]["resolution_id"])
            require(source.specs[name] == spec, "admission_retained", "Retained generation differs from its selected specification.")
        generation = store.durable / "generations" / (canonical_digest(record_document(spec)) + "-" + checksum + ".json")
        admission.protected_directory(host, generation.parent, 0o700)
        generations[name] = {"unit": asdict(spec.identity), "resolution_id": source.resolution.id,
                            "launch_file": entry["allocations"]["paths"]["application"] + "/runtime/" + source.resolution.id + "/launch.json"}
        atomic_json(generation, generations[name], mode=0o600)
        declaration = runtime_declaration(source_launch["declaration"])
        role = source.roles[name]
        paths = services.ServicePaths.for_unit(spec.identity, source.resolution.id)
        views = services.bundle_views(role, paths, source.topology, source.profile.architecture,
                                      source_launch["artifact_roots"], declaration.artifacts)
        runtime_config.prepare_unit_paths(host, role, spec, source.resolution, views=views)
        content = services.render_unit(source, name, entry, checksum, views=views,
                                       secret_paths=source_launch["secret_paths"].get(name, {}))
        provision._owned_write(host, host.paths.p("/etc/systemd/system/" + inventory.units[name]), content)
    spec = inventory.specs["http"]
    command = services.command_line(["/usr/bin/python3", "-I", services.RUNTIME_ENTRY, "boot", spec.identity.instance_id,
                                    "http", "1", generations["http"]["resolution_id"], checksum, canonical_digest(record_document(spec))])
    content = (Path(tools_dir) / "server/templates/admission.service").read_text()
    content = content.replace("@MARKER@", marker(entry)).replace("@BOOT@", command)
    boot_unit = "foundry-admission-" + spec.identity.instance_id + ".service"
    provision._owned_write(host, host.paths.p("/etc/systemd/system/" + boot_unit), content)
    host.run(["systemctl", "daemon-reload"])
    opener = "foundry-admission-open-" + spec.identity.instance_id + ".service"
    content = content.replace("Description=", "Description=Open ").replace("RemainAfterExit=yes", "RemainAfterExit=no")
    content = content.replace('"boot"', '"open"').replace("After=network-online.target ufw.service nginx.service",
              "After=" + boot_unit + " " + " ".join(inventory.units.values()))
    content = content.replace("Requires=nginx.service", "Requires=nginx.service " + boot_unit)
    provision._owned_write(host, host.paths.p("/etc/systemd/system/" + opener), content)
    host.run(["systemctl", "daemon-reload"])
    host.run(["systemctl", "enable", boot_unit, opener])


def stop_units(host, inventory, names=None):
    names = set(inventory.roles) if names is None else set(names)
    for name in inventory.stop_order(names):
        unit = inventory.units[name]
        # StopUnit fails for a candidate that has never been installed. Query
        # that case explicitly; never interpret a failed stop as proof of exit.
        if services.unit_state(host, unit)["LoadState"] != "not-found":
            host.run(["systemctl", "stop", unit], timeout=100)
        services.stopped_unit(host, unit)


def start_units(host, inventory, names, deadline):
    """Takeover/lifecycle interface: start an explicit enabled graph without enabling it."""
    names = set(names)
    enabled = services.enabled_units(host, inventory.units)
    require(names <= set(enabled), "service_enablement", "Cannot start a deliberately disabled role.")
    order = inventory.start_order(enabled)
    for prerequisite in inventory.prerequisites(names) - names:
        require(services.wait_ready(host, inventory, prerequisite, deadline),
                "service_prerequisite", "A retained prerequisite is unavailable.")
    for name in order:
        if name in names:
            require(time.monotonic() < deadline, "service_deadline", "Startup exhausted its shared deadline.")
            host.run(["systemctl", "start", inventory.units[name]], timeout=max(.001, deadline - time.monotonic()))
            require(services.wait_ready(host, inventory, name, deadline), "readiness_failed", "Selected role failed readiness.")


def ready(host, entry, inventory, name, deadline):
    return services.wait_ready(host, inventory, name, deadline)


def current_executor(host):
    invocation = os.environ.get("INVOCATION_ID", "")
    stat = host.paths.p(f"/proc/{os.getpid()}/stat").read_text().rsplit(") ", 1)[1].split()
    return ProcessIdentity.parse({"pid": os.getpid(), "start_ticks": int(stat[19]), "invocation_id": invocation})



def configuration_hold_running(host, entry):
    path = admission.AdmissionStore(host, entry["profile"]["instance_id"]).durable / "configuration-hold.json"
    if not path.exists():
        return None
    held = admission.read_owned(host, path, lambda value: value)
    require(held["private_revision"] == entry["configuration_revision"],
            "configuration_reconciliation", "Configuration hold revision differs.")
    return set(held["running_roles"])


def transition_enablement(host, entry, inventory, *, restoring=False):
    import release
    if "deployment_selection" not in entry:
        return tuple(inventory.roles), tuple(inventory.roles), {}
    _, _, previous, _ = load(host, entry)
    enabled = set(services.enabled_units(host, previous.units))
    path = admission.AdmissionStore(host, inventory.profile.instance_id).durable / "removed-enablement.json"
    removed = admission.read_owned(host, path, lambda v: v) if path.exists() else {}
    require(isinstance(removed, dict) and all(isinstance(k, str) and type(v) is bool for k, v in removed.items()),
            "service_enablement", "Invalid removed-copy enablement evidence.")
    desired = {name for name in inventory.roles if name in enabled or name not in previous.roles and
               (removed.get(name, False) if restoring or name in removed else True)}
    held_running = configuration_hold_running(host, entry)
    running = {name for name in desired if name not in previous.roles or
               (name in held_running if held_running is not None else
                release.service(host, previous.units[name])["ActiveState"] == "active")}
    while any(set(inventory.roles[name].common.requires) - desired for name in desired):
        desired = {name for name in desired if set(inventory.roles[name].common.requires) <= desired}
    running &= desired
    while any(set(inventory.roles[name].common.requires) - running for name in running):
        running = {name for name in running if set(inventory.roles[name].common.requires) <= running}
    inventory.start_order(desired)
    inventory.start_order(running)
    return tuple(sorted(desired)), tuple(sorted(running)), {name: name in enabled for name in previous.roles if name not in inventory.roles}

def transition(host, registry, entry, inventory, state, attempt, *, stop_only=False,
               names=None, configuration=None, launch=None, review=None, maintenance_request=None):
    import release
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    old = entry.get("deployment_selection", {}).get("selected_resolution_id", inventory.resolution.id)
    previous = load(host, entry)[2] if "deployment_selection" in entry else inventory
    names = set(names) if names is not None else set(previous.roles) | set(inventory.roles)
    retained = set(inventory.roles) - names
    if review is not None:
        enabled, running, removed = transition_enablement(host, entry, inventory, restoring=review["restoring"])
        require(list(enabled) == review["enabled_roles"] and list(running) == review["running_roles"],
                "stale_plan", "Enablement or running selection changed after review.")
    else:
        enabled = services.enabled_units(host, previous.units) if "deployment_selection" in entry else tuple(inventory.roles)
        running, removed = set(enabled), {}
        if attempt["action"] == "config" or maintenance_request:
            held_running = configuration_hold_running(host, entry)
            if held_running is not None:
                running = held_running & set(enabled)
            else:
                running = {name for name in enabled if release.service(host, previous.units[name])["ActiveState"] == "active"}
    inventory.start_order(enabled)
    holds, running, maintenance_intent = feature_maintenance.selection(
        host, entry, inventory, enabled, running, maintenance_request["action"] if maintenance_request else None,
        allow_start=not (maintenance_request and maintenance_request.get("maintenance_no_start")))
    maintenance_intent["operation_id"] = attempt["operation_id"]
    running = set(running)
    if review is not None:
        require(sorted(holds) == review["maintenance_holds"] and sorted(running) == review["maintenance_running_roles"],
                "stale_plan", "Maintenance holds or restored running roles changed after review.")
    inventory.start_order(running)
    attempt["running_roles"] = sorted(running)
    host.save_state(entry, state)
    selected = DeploymentSelection(inventory.profile.instance_id, FEATURE_LIFECYCLE_GENERATION, tuple(sorted(FEATURE_CAPABILITIES)),
                 inventory.resolution.id, None if old == inventory.resolution.id else old,
                 AdmissionRecord(inventory.profile.instance_id, inventory.resolution.id, inventory.resolution.deployment_sha256,
                                 inventory.resolution.private_revision, attempt["operation_id"], AdmissionDisposition.PENDING),
                 tuple(enabled), tuple(sorted(holds)))
    request = transition_request(entry, inventory, attempt["operation_id"], names=names, previous=previous)
    if retained:
        store.retain_units(previous, inventory, retained, attempt["operation_id"])
    operations.bind_transition(host, entry, inventory, request)
    store.record_operation(attempt)
    if removed:
        removed_path = store.durable / "removed-enablement.json"
        prior = admission.read_owned(host, removed_path, lambda v: v) if removed_path.exists() else {}
        atomic_json(removed_path, {**prior, **removed}, mode=0o600)
    # Intent precedes the shared command, marker publication and every stop.
    atomic_json(operations.directory(host, attempt["operation_id"]) / "maintenance-intent.json", maintenance_intent, mode=0o600)
    maintenance_result = feature_maintenance.shared_change(host, entry, maintenance_request) if maintenance_request else {}
    if maintenance_intent["holds"]:
        atomic_json(store.durable / "maintenance.json", maintenance_intent, mode=0o600)
    feature_maintenance.publish_markers(host, inventory, holds)
    store.begin(selected, inventory.resolution)
    store._write(store.durable / "transition.json", request)
    units = {**previous.units, **inventory.units}
    feature_network.withdraw(host, entry, request, inventory.resolution, units)
    stop_units(host, previous, names & previous.roles.keys())
    evidence = feature_network.close(host, entry, request, inventory.resolution, units)
    store.record_boot_closure(evidence)
    store._write(store.durable / "transition.json", replace(request, closure_id=evidence.id))
    atomic_json(operations.directory(host, attempt["operation_id"]) / "validated.json", {"operation_id": attempt["operation_id"]}, mode=0o600)
    feature_maintenance.clear_markers(host, inventory, maintenance_intent["holds"])
    feature_maintenance.clear_markers(host, previous, maintenance_intent["holds"])
    http = next(e for e in inventory.resolution.endpoints if e.role == "http" and e.listener == "http")
    entry["deployment_selection"] = record_document(selected)
    entry["topology"] = {**inventory.topology.projection(), "sha256": inventory.topology.sha256}
    entry["profile"] = inventory.profile.projection()
    entry["allocations"]["http_port"] = http.port
    if attempt.get("redis_inventory") is not None:
        release.phase(host, entry, state, "redis")
        store.record_operation(attempt)
        release.widen_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
    if configuration is not None:
        from contracts import reserve_runtime_storage
        _, values = runtime_config.read_configuration_revision(host, entry, configuration)
        reserve_runtime_storage(registry, entry, values, lambda value: runtime_config._read_value_file(host, entry, value))
        host.journal(registry, entry)
        release.phase(host, entry, state, "configuration")
        store.record_operation(attempt)
        runtime_config.publish_configuration_revision(host, registry, entry, configuration)
        attempt["configuration_applied" if attempt["action"] == "config" else "private_configuration_applied"] = True
        state["configuration_revision"] = entry["configuration_revision"]
        from contracts import select_runtime_storage
        select_runtime_storage(registry, entry, runtime_config.validate_runtime_environment(host, entry),
                               lambda value: runtime_config._read_value_file(host, entry, value))
    host.journal(registry, entry)
    for name in set(previous.roles) - set(inventory.roles):
        host.run(["systemctl", "disable", previous.units[name]])
    if launch is not None:
        install_units(host, registry, entry, inventory, launch, Path(__file__).resolve().parents[1])
    path = release.release_path(host, entry, inventory.deployment.release)
    env = runtime_config.effective_environment_path(host, entry, inventory.resolution)
    if attempt["action"] == "deploy":
        if release.migration_status(host, entry, path, env_file=env):
            if state["previous"]:
                release.phase(host, entry, state, "backup")
                store.record_operation(attempt)
                attempt["backup"] = release.backup_database(host, entry, attempt["operation_id"])
            release.phase(host, entry, state, "migration")
            store.record_operation(attempt)
            release.app(host, entry, path, "migrate", env_file=env, extra=("--database", "primary"), timeout=900)
        release.phase(host, entry, state, "seed")
        store.record_operation(attempt)
        release.app(host, entry, path, "db:seed", env_file=env, extra=("--database", "primary"), timeout=900)
    release.phase(host, entry, state, "switch")
    store.record_operation(attempt)
    record_selection(host, entry, state, inventory.deployment.release)
    release.switch_to(host, entry, inventory.deployment.release)
    if configuration is not None:
        release.phase(host, entry, state, "backup")
        store.record_operation(attempt)
        release.backup.archive_keys(host, entry)
    if stop_only:
        host.run(["systemctl", "disable", *inventory.units.values()])
        selected = replace(selected, enabled_roles=())
        store._write(store.durable / "selection.json", selected)
    else:
        if enabled:
            host.run(["systemctl", "enable", *(inventory.units[name] for name in enabled)])
        for name in set(inventory.roles) - set(enabled):
            host.run(["systemctl", "disable", inventory.units[name]])
        attempt.update(phase="health")
        host.save_state(entry, state)
        store.record_operation(attempt)
        starting = running & names
        if starting:
            permit = ValidationPermit(str(uuid.uuid4()), attempt["operation_id"], admission.boot_id(host), inventory.resolution.id,
                                      evidence.id, current_executor(host), tuple(inventory.specs[name].identity for name in sorted(starting)))
            store.issue_permit(permit, evidence)
            atomic_write(store.runtime / "active-permit", permit.id, mode=0o600)
        deadline = time.monotonic() + release.READINESS_SECONDS
        start_units(host, inventory, starting, deadline)
        require(not running or services.wait_ready_set(host, inventory, running, deadline),
                "readiness_failed", "The selected running set did not remain ready before public opening.")
        if running:
            feature_network.open_public(host, entry, inventory.resolution, evidence)
    state["status"] = "stopped" if stop_only or not enabled else (
        "healthy" if running | holds == set(inventory.roles) else "degraded")
    if attempt.get("redis_inventory") is not None:
        release.queue_redis_narrowing(state, attempt["redis_inventory"])
    atomic_json(store.durable / "maintenance.json", maintenance_intent, mode=0o600)
    release.finish(host, entry, state, "succeeded")
    store.record_operation(state["last_attempt"])
    store.commit()
    active_permit = store.runtime / "active-permit"
    if active_permit.exists():
        store.revoke_permit(contract_uuid(active_permit.read_text()))
        active_permit.unlink()
    if configuration is not None or review is not None:
        (store.durable / "configuration-hold.json").unlink(missing_ok=True)
    entry["deployment_selection"] = record_document(store._selection()[0])
    enablement = store.durable / "enablement.json"
    if not enablement.exists():
        atomic_json(enablement, {"schema": 1, "authority": "systemd", "state": "complete"}, mode=0o600)
    host.journal(registry, entry)
    if attempt.get("redis_inventory") is not None:
        release.narrow_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
    return maintenance_result


def boot(host, entry, inventory):
    """Boot only reconstructs closure; it never replays a pending operation."""
    with host.lock("host"), host.lock(inventory.profile.instance_id):
        store = admission.AdmissionStore(host, inventory.profile.instance_id)
        selected, resolution = store._selection()
        request = HostTransition(selected.admission.operation_id, inventory.profile.instance_id, resolution.id, resolution.id,
                                 resolution.private_revision, tuple(s.identity for s in inventory.specs.values()),
                                 canonical_digest(record_document(inventory.deployment)), None)
        evidence = feature_network.close(host, entry, request, resolution, inventory.units)
        store.record_boot_closure(evidence)
        origins = store._retained_records(resolution)
        for name, spec in inventory.specs.items():
            origin = origins.get(name, {}).get("resolution_id", resolution.id)
            source, launch = load_resolution(host, entry, origin)
            from release import runtime_declaration
            declaration = runtime_declaration(launch["declaration"])
            role = source.roles[name]
            paths = services.ServicePaths.for_unit(spec.identity, origin)
            views = services.bundle_views(role, paths, source.topology, source.profile.architecture,
                                          launch["artifact_roots"], declaration.artifacts)
            runtime_config.prepare_unit_paths(host, role, spec, source.resolution, views=views)


def open_boot(host, entry, inventory):
    from contracts import ClosureEvidence
    import release
    with host.lock("host"), host.lock(inventory.profile.instance_id):
        store = admission.AdmissionStore(host, inventory.profile.instance_id)
        selection, resolution = store._selection()
        require(selection.admission.disposition is AdmissionDisposition.COMMITTED,
                "admission_pending", "A pending operation requires reconciliation before boot startup.")
        store.require_cleanup_clear()
        store._completed(selection)
        enabled = tuple(sorted(set(services.enabled_units(host, inventory.units)) - set(selection.maintenance_holds)))
        inventory.start_order(enabled)
        if not enabled:
            return
        evidence = admission.read_owned(host, store.runtime / "boot.json", ClosureEvidence.parse)
        deadline = time.monotonic() + release.READINESS_SECONDS
        require(services.wait_ready_set(host, inventory, enabled, deadline),
                "readiness_failed", "The complete enabled set did not remain ready before boot opening.")
        feature_network.open_public(host, entry, resolution, evidence)


def configuration_request(host, project, profile, changes):
    import release
    with host.lock("host"), host.lock(profile["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        migrate_enablement(host, registry, entry)
        release.require_idle(host.load_state(entry))
        store, _, inventory, _ = load(host, entry)
        store.require_cleanup_clear()
        runtime_config.validate_configuration_changes(changes, entry)
        application, _ = runtime_config.configuration_parts(changes, entry)
        identifier = runtime_config.stage_configuration_revision(host, entry, changes, inventory.topology, inventory.profile)
        record, values = runtime_config.read_configuration_revision(host, entry, identifier)
        if (record["paths"] == runtime_config.service_secret_snapshot(host, entry)
                and values == runtime_config.validate_runtime_environment(host, entry)
                and not (store.durable / "configuration-hold.json").exists()):
            return {"ok": True, "unchanged": True, "configuration_revision": entry["configuration_revision"]}
    return request_operation(host, "config-set", project, profile, configuration_intent=identifier)


def configuration_candidate(host, entry, state, request):
    import release
    _, _, previous, prior_launch = load(host, entry)
    record, values = runtime_config.read_configuration_revision(host, entry, request["configuration_intent"])
    manifest = state["current"]
    path = release.verified_release(host, entry, manifest, reviewed=True)
    declaration = release.runtime_declaration(prior_launch["declaration"])
    paths = runtime_config.service_secret_paths(host, entry, previous.topology, paths=record["paths"], profile=previous.profile)
    affected = {name for name in previous.roles if paths.get(name, {}) != prior_launch["secret_paths"].get(name, {})}
    if record["application_keys"] or (admission.AdmissionStore(host, previous.profile.instance_id).durable / "configuration-hold.json").exists():
        affected.update(previous.roles)
    require(affected, "invalid_configuration", "Configuration intent has no changed effective inputs.")
    inventory, paths = resolve(host, entry, manifest, declaration, path, prior_launch["artifact_roots"], request["operation_id"],
        private_paths=record["paths"], values=values, revision=record["revision"], previous=previous, affected=affected,
        profile=previous.profile.projection())
    launch = save_launch(host, entry, inventory, declaration, prior_launch["artifact_roots"], paths)
    env = runtime_config.effective_environment_path(host, entry, inventory.resolution, candidate=True)
    release.app(host, entry, path, "config:check", env_file=env)
    if record["application_keys"]:
        release.require_configuration_keys(host, entry, path, env, record["application_keys"])
    affected = {name for name in inventory.roles if inventory.specs[name] != previous.specs[name]
                or next(e.sha256 for e in inventory.resolution.environments if e.role == name) !=
                   next(e.sha256 for e in previous.resolution.environments if e.role == name)}
    if (admission.AdmissionStore(host, previous.profile.instance_id).durable / "configuration-hold.json").exists():
        affected = set(inventory.roles)
    if any(key.startswith(release.STORAGE_CONFIGURATION) for key in record["application_keys"]):
        release.probe_storage(host, entry, path, env)
    redis_inventory = release.release_redis_inventory(host, entry, path, env_file=env)
    attempt = release.begin(host, entry, state, "config", manifest["version"], metadata={
        "operation_id": request["operation_id"], "configuration_revision": record["revision"],
        "configuration_sha256": record["sha256"], "previous_configuration_sha256": record["previous_sha256"]})
    attempt["redis_inventory"] = redis_inventory
    return inventory, launch, affected, attempt


def prepare_release(host, entry, state, request, *, recovery_declaration=None):
    import release
    archive = check_path(Path(request["archive"]), "file")
    manifest = release.unpack_archive(host, archive, None, entry, request["archive_sha256"], reviewed=True)
    if recovery_declaration is not None:
        require(manifest["deployment"] == recovery_declaration and
                manifest["commit"] == entry["recovered_from"]["release_commit"],
                "recovery_release_changed", "Recovery requires the exact archived declaration and release commit.")
    stage_root = operations.staging(host, entry, request["operation_id"])
    admission.protected_directory(host, stage_root, 0o700)
    staging = stage_root / "release"
    budget = artifacts.ExpansionBudget()
    manifest = release.unpack_archive(host, archive, staging, entry, request["archive_sha256"],
                                      expansion_budget=budget, reviewed=True)
    declaration = ReleaseDeclaration.parse(manifest["deployment"])
    topology = parse_topology(declaration.topology_json)
    profile_record = parse_profile(entry["profile"] if recovery_declaration is not None else
        release.target_declaration(host, entry, manifest)["profile"], topology)
    feature_maintenance.stopped_roles(topology)
    require(release.legacy_profile(profile_record.projection()) == release.legacy_profile(entry["profile"]),
            "release_identity", "Reviewed releases cannot change the instance host or infrastructure profile.")
    release.persist_declaration(host, entry, manifest, topology.projection(), profile_record.projection(), request["archive_sha256"])
    plan = packages.inspect(host, feature_network.package_topology(topology), profile_record)
    require(not plan["missing"], "capability_unavailable", f"New package requirements after bootstrap need {LATER}; rebuild this preview host.")
    prepared = artifacts.verify_release_tree(declaration, staging, budget,
                 artifact_staging=stage_root / "artifacts", outer_charged=True)
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    history_path = store.durable / "artifact-history.json"
    history = tuple(artifacts.ArtifactReference(**item) for item in read_json(history_path)) if history_path.exists() else ()
    refs = artifacts.references(declaration)
    artifacts.validate_history(refs, history)
    admission.protected_directory(host, history_path.parent, 0o700)
    atomic_json(history_path, [asdict(ref) for ref in sorted(set(history) | set(refs))], mode=0o600)
    roots = {}
    for prepared_artifact in prepared:
        artifact = prepared_artifact.artifact
        abi = None
        if artifact.bundles:
            consumers = [role for role in topology.roles if isinstance(role, ExecutableRole) and
                         any(m.artifact == prepared_artifact.payload.artifact for _, m in role.common.artifact_mounts)]
            versions = {packages.executable_evidence(host, role.executable)["version"] for role in consumers
                        if isinstance(role.executable, PackageExecutable)}
            require(len(versions) == 1 and len(consumers) == sum(isinstance(r.executable, PackageExecutable) for r in consumers),
                    "capability_unavailable", f"Unverified service bundle ABI requires {LATER}.")
            abi = next(iter(versions))
        tree = artifacts.publish(host, entry["name"], prepared_artifact, history=history, observed_service_abi=abi)
        reference = next(ref for ref in refs if ref.artifact == prepared_artifact.payload.artifact)
        for bundle_name, _ in artifact.bundles:
            artifacts.bundle_export(reference, artifact, tree, bundle_name, prepared_artifact.payload.members,
                                    observed_service_abi=abi, uid=os.geteuid())
        roots[prepared_artifact.payload.artifact] = (absolute(host, tree), prepared_artifact.payload.sha256)
    release_path = release.release_path(host, entry, manifest["version"])
    if release_path.exists():
        release.verified_release(host, entry, manifest, reviewed=True)
    else:
        os.rename(staging, release_path)
        fd = os.open(release_path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    return manifest, declaration, release_path, roots


def execute(host, request, *, pin=None):
    """Run inside the owned operation unit, after the SSH observer has handed off."""
    import release
    action = request["action"]
    check_operation(action)
    project, profile = request["project"], request["profile"]
    operations.verify_executor(host, request, pin)
    feature_network.capability()
    with host.lock("host"), host.lock(profile["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        require(is_feature(entry) and registry["tools"]["generation"] >= FEATURE_LIFECYCLE_GENERATION,
                "capability_unavailable", "Feature execution requires the generation-13 lifecycle release.")
        state = host.load_state(entry)
        migrate_enablement(host, registry, entry)
        admission.AdmissionStore(host, profile["instance_id"]).require_cleanup_clear(request["operation_id"])
        release.require_idle(state)
        inventory, attempt, names, review = None, None, None, None
        prior_status, prior_current, prior_previous = state["status"], state["current"], state["previous"]
        try:
            if action in ("down", "up"):
                inventory, launch, names = feature_maintenance.candidate(host, entry, state, request)
                attempt = release.begin(host, entry, state, "restart", inventory.deployment.release,
                    metadata={"operation_id": request["operation_id"], "maintenance_action": action})
                if not names:
                    # No held copies: HTTP and every continuing unit retain admission.
                    store = admission.AdmissionStore(host, profile["instance_id"])
                    maintenance_result = feature_maintenance.shared_change(host, entry, request)
                    enabled = services.enabled_units(host, inventory.units)
                    if enabled:
                        require(services.wait_ready_set(host, inventory, enabled, time.monotonic() + release.READINESS_SECONDS),
                                "readiness_failed", "Maintenance changed but the running graph did not become ready.")
                    release.finish(host, entry, state, "succeeded")
                    store.record_operation(state["last_attempt"])
                    return {"ok": True, "maintenance": {"down": action == "down"},
                            **({"bypass_path": maintenance_result["bypass_path"]}
                               if "bypass_path" in maintenance_result else {})}
            elif action == "config-set":
                inventory, launch, names, attempt = configuration_candidate(host, entry, state, request)
            elif action not in ("activate", "rollback"):
                store, selected, inventory, launch = load(host, entry)
                require(request.get("role", "all") == "all", "capability_unavailable", f"Named feature operations require {LATER}.")
                attempt = release.begin(host, entry, state, "restart" if action == "restart" else "stop", inventory.deployment.release,
                                        metadata={"operation_id": request["operation_id"]})
            else:
                import feature_plan
                manifest, inventory, launch, configuration, review = feature_plan.reviewed(host, registry, entry, request, pin=pin)
                request = {**request, "configuration_intent": configuration}
                release_path = release.release_path(host, entry, manifest["version"])
                env = runtime_config.effective_environment_path(host, entry, inventory.resolution, candidate=True)
                release.native_runtime(host, entry)
                release.protect_maps(host, entry, release_path)
                patterns = release.release_redis_inventory(host, entry, release_path, env_file=env)
                if "deployment_selection" not in entry and prior_current is None:
                    atomic_json(operations.directory(host, request["operation_id"]) / "initial-validation.json",
                                {"status": prior_status, "current": prior_current, "previous": prior_previous,
                                 "configuration_revision": entry["configuration_revision"]}, mode=0o600)
                attempt = release.begin(host, entry, state, "rollback" if review["restoring"] else "deploy", manifest["version"], metadata={"operation_id": request["operation_id"]})
            store = admission.AdmissionStore(host, profile["instance_id"])
            store.record_operation(attempt)
            if action in ("activate", "rollback"):
                attempt["redis_inventory"] = patterns
                state["previous"], state["current"] = state["current"], manifest
            maintenance_result = transition(host, registry, entry, inventory, state, attempt, stop_only=action == "stop", names=names,
                       configuration=request.get("configuration_intent"), launch=launch if action in ("activate", "rollback", "config-set", "down", "up") else None, review=review,
                       maintenance_request=request if action in ("down", "up") else None)
        except (DeployError, OSError):
            if attempt is None:
                raise
            store = admission.AdmissionStore(host, profile["instance_id"])
            if not (operations.directory(host, request["operation_id"]) / "transition.json").exists():
                state.update(status=prior_status, current=prior_current, previous=prior_previous)
                release.finish(host, entry, state, "failed", "feature_validation_failed")
                store.record_operation(state["last_attempt"])
                raise
            applied = attempt["action"] == "config" and attempt.get("configuration_applied") is True
            if applied:
                atomic_json(store.durable / "configuration-hold.json", {"operation_id": attempt["operation_id"],
                    "private_revision": entry["configuration_revision"], "running_roles": attempt["running_roles"]}, mode=0o600)
            maintenance_failed = action in ("down", "up")
            state["status"] = "failed" if applied or maintenance_failed else "unknown"
            release.finish(host, entry, state, "failed_applied" if applied else "failed" if maintenance_failed else "needs_reconciliation",
                           "configuration_readiness" if applied else "maintenance_readiness" if maintenance_failed else "feature_transition_pending")
            store.record_operation(state["last_attempt"])
            # Cleanup must still prove exit and closure before corrective configuration.
            raise
        return {"ok": True, "instance": entry["name"], "version": inventory.deployment.release,
                **({"maintenance": {"down": action == "down"},
                    **({"bypass_path": maintenance_result["bypass_path"]}
                       if "bypass_path" in maintenance_result else {})} if action in ("down", "up") else {}),
                "status": state["status"], "profile_update": entry["profile"],
                "retention": {"error": {"code": "capability_unavailable", "detail": f"Feature helper pruning requires {LATER}; all bundles retained."}}}


def request_operation(host, action, project, profile, *, operation_id=None, **arguments):
    check_operation(action)
    feature_network.capability()
    operation_id = contract_uuid(operation_id or str(uuid.uuid4()))
    with host.lock("host"):
        registry, entry = host.require_identity(project, profile)
        directory = operations.directory(host, operation_id)
        request = {"schema": 1, "action": action, "operation_id": operation_id, "project": project, "profile": profile, **arguments}
        path = directory / "request.json"
        if path.exists():
            previous_request = read_json(path)
            if (directory / "result.json").exists():
                import release
                require(release.legacy_profile(previous_request["profile"]) == release.legacy_profile(profile),
                        "operation_identity", "Operation profile identity changed.")
                request["profile"] = previous_request["profile"]
            require(previous_request == request, "operation_identity", "Operation UUID belongs to another request.")
            if (directory / "result.json").exists():
                admission.AdmissionStore(host, profile["instance_id"]).require_cleanup_clear()
                receipt = read_json(directory / "result.json")
                if "error" in receipt:
                    raise DeployError(receipt["error"]["code"], receipt["error"]["detail"])
                return receipt
            refuse("interrupted-operation reconciliation")
        store = admission.AdmissionStore(host, profile["instance_id"])
        store.require_cleanup_clear()
        require(action not in ("restart", "down", "up") or not (store.durable / "configuration-hold.json").exists(),
                "configuration_held", "The published configuration is held; submit corrective config or a reviewed release transition.")
        if action in ("activate", "rollback"):
            import release
            state = host.load_state(entry)
            release.require_idle(state)
            import feature_plan
            feature_plan.reviewed(host, registry, entry, request)
        owner = operations.prepare(host, registry, entry, request)
    return operations.launch(host, request, owner)


def prepare_recovery(host, registry, entry, journal, snapshot, archive, checksum, tools_dir):
    from feature_recovery import prepare
    return prepare(host, registry, entry, journal, snapshot, archive, checksum, tools_dir)


def activate_recovery(host, registry, entry, journal):
    from feature_recovery import activate
    return activate(host, registry, entry, journal)


def recovery_ready(host, entry, journal):
    from feature_recovery import ready
    return ready(host, entry, journal)
