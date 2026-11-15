"""Feature lifecycle: reviewed transitions, private configuration and scoped admission."""
from dataclasses import asdict, replace
import json
import os
from pathlib import Path
import re
import shutil
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
from copy_enablement import record_removed_copy_enablement
from contracts import unit_selection_names, unit_selection_identities
from contracts import (AdmissionDisposition, AdmissionRecord, Deployment,
                       DeploymentSelection, DeployError, FEATURE_CAPABILITIES, FEATURE_LIFECYCLE_GENERATION, HostTransition,
                       ProcessIdentity, ReleaseDeclaration, ResolvedBundleBinding, ResolvedEndpointBinding,
                       ResolvedExecutableBinding, RoleBindings, RuntimeResolution, UnitIdentity, UnitSpec,
                       ValidationPermit, atomic_json, atomic_write, canonical_digest, check_path, contract_uuid,
                       digest, marker, read_json, record_document, require)
from topology import (ApplicationExecutable, ArtifactExecutable, BuiltinRole, BundleDirectoryBinding,
                      CopiesBinding, EndpointBinding, ExecutableBinding, ExecutableRole, PackageExecutable, PublicListenerReference, parse_profile, parse_topology, expand_placement)

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
    intent = {"schema": 1, "authority": "systemd", "state": "prepared", "enabled": sorted(unit_selection_names(enabled)),
              "adopt_running": sorted(unit_selection_names(set(running) - enabled)), "starts": []}
    admission.protected_directory(host, path.parent, 0o700)
    atomic_json(path.with_name("enablement-intent.json"), intent, mode=0o600)
    install_units(host, registry, entry, inventory, launch, Path(__file__).resolve().parents[1])
    if intent["adopt_running"]:
        host.run(["systemctl", "enable", *(inventory.units[name] for name in unit_selection_identities(inventory.profile.instance_id, intent["adopt_running"]))])
    result = {**intent, "state": "complete", "enabled": list(unit_selection_names(services.enabled_units(host, inventory.units)))}
    atomic_json(path, result, mode=0o600)
    return result


def network_report(host, entry, host_claims, host_confinement=None):
    from contracts import ClosureEvidence
    from report import declared_network
    if "deployment_selection" not in entry:
        topology = parse_topology(entry["topology"], registered=True)
        return declared_network(host, parse_profile(entry["profile"], topology), topology=topology,
                                entry=entry, host_claims=host_claims, host_confinement=host_confinement)
    store, selection, inventory, _ = load(host, entry)
    require(entry["deployment_selection"] == record_document(selection),
            "network_selection", "Registry and private network selection differ.")
    path = store.runtime / "boot.json"
    closure = admission.read_owned(host, path, ClosureEvidence.parse) if path.exists() else None
    return declared_network(host, inventory.profile, topology=inventory.topology, entry=entry,
                            resolution=inventory.resolution, closure=closure, host_claims=host_claims, host_confinement=host_confinement)


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


def changed_release_units(host, entry, previous, prior_launch, declaration, release_path, roots,
                          profile, private_paths):
    """Compare effective inputs before assigning a new runtime generation."""
    import release
    topology = parse_topology(declaration.topology_json)
    expanded = expand_placement(topology, profile.instance_id, profile.placement)
    prior_declaration = release.runtime_declaration(prior_launch["declaration"])
    secrets = runtime_config.unit_secret_paths(host, entry, topology, paths=private_paths, profile=profile)
    old_members = {member.path: member for member in prior_declaration.inventory}
    members = {member.path: member for member in declaration.inventory}
    def application_members(inventory):
        return {name: member for name, member in inventory.items()
                if name in ("artisan", "config/deploy.toml") or name.startswith("public/")}
    application_changed = application_members(old_members) != application_members(members)
    from listeners import copy_public_bindings
    changed = set(expanded) ^ set(previous.units)
    for identity in set(expanded) & previous.units.keys():
        role = expanded[identity]
        if (role != previous.roles[identity.role] or profile.architecture != previous.profile.architecture
                or topology.shared_directories != previous.topology.shared_directories
                or copy_public_bindings(previous.profile, identity) != copy_public_bindings(profile, identity)):
            changed.add(identity)
            continue
        spec = previous.specs[identity]
        paths = services.ServicePaths.for_unit(identity, previous.resolution.id)
        views = services.bundle_views(role, paths, topology, profile.architecture, roots, declaration.artifacts)
        old_views = services.bundle_views(role, paths, previous.topology, previous.profile.architecture,
                                        prior_launch["artifact_roots"], prior_declaration.artifacts)
        if views != old_views:
            changed.add(identity)
        if isinstance(role, BuiltinRole):
            if application_changed or spec.executable_sha256 != digest(release_path / "artisan"):
                changed.add(identity)
        else:
            if (executable(host, role.executable, declaration, release_path, roots)[1] != spec.executable_sha256
                    or any(members.get(config.source) != old_members.get(config.source) for _, config in role.configs)
                    or secrets.get(identity, {}) != prior_launch["secret_paths"].get(identity.record_key, {})):
                changed.add(identity)
        for name, binding in role.common.bindings:
            if isinstance(binding, ExecutableBinding):
                prior = next(value for value in spec.bindings.executables if value.name == name)
                path, checksum = executable(host, binding.executable, declaration, release_path, roots)
                if checksum != prior.source_sha256 or (not isinstance(binding.executable, ApplicationExecutable) and path != prior.path):
                    changed.add(identity)
    return changed


def changed_units(previous, inventory):
    """Return additions, removals and changed effective specs in one identity set."""
    changed = set(previous.units) ^ set(inventory.units)
    old_environments = {value.role: value.sha256 for value in previous.resolution.environments}
    environments = {value.role: value.sha256 for value in inventory.resolution.environments}
    changed.update(identity for identity in set(previous.units) & inventory.units.keys()
                   if previous.specs[identity] != inventory.specs[identity]
                   or old_environments[identity.role] != environments[identity.role])
    return changed


def resolve(host, entry, manifest, declaration, release_path, roots, operation_id, *,
            private_paths=None, values=None, revision=None, previous=None, affected=None, profile=None, preserve_private=False):
    topology = parse_topology(declaration.topology_json)
    profile = parse_profile(profile if profile is not None else declaration.profile_json, topology)
    expanded = expand_placement(topology, profile.instance_id, profile.placement)
    affected = set(affected) if affected is not None else set(expanded)
    reusable = {identity for identity, role in expanded.items()
                if preserve_private and previous is not None and identity in previous.units
                and isinstance(role, ExecutableRole)
                and all(listener.bind == "loopback" for _, listener in role.listeners if listener.transport != "unix")}
    if previous is not None:
        previous_public = dict(previous.profile.listeners)
        current_public = dict(profile.listeners)
        affected.update(unit for unit, role in expanded.items() for _, binding in role.common.bindings
                        if isinstance(binding, PublicListenerReference) and
                        previous_public.get(binding.role + "." + binding.listener) !=
                        current_public.get(binding.role + "." + binding.listener))
        # Listener consumers have changed effective inputs when their provider moves.
        while True:
            consumers = {unit for unit, role in expanded.items() for _, binding in role.common.bindings
                         if (isinstance(binding, (EndpointBinding, PublicListenerReference))
                             and UnitIdentity(profile.instance_id, binding.role, 1) in affected - reusable
                             or isinstance(binding, CopiesBinding) and any(identity.role == binding.role for identity in affected - reusable))}
            if consumers <= affected:
                break
            affected.update(consumers)
    retained = (set(previous.units) & expanded.keys()) - affected if previous else set()
    endpoints, nginx = feature_network.allocate(host, entry, topology, profile, operation_id,
        retained=tuple(e for e in previous.resolution.endpoints if UnitIdentity(profile.instance_id, e.role, e.ordinal) in retained | reusable) if previous else None)
    resolution_id = str(uuid.uuid4())
    roles = tuple(role.common.name for role in topology.roles if dict(profile.placement)[role.common.name])
    environments = runtime_config.effective_environments(host, entry, resolution_id,
                   roles, endpoints, values=values)
    if previous:
        prior_environments = {value.role: value for value in previous.resolution.environments}
        changed_environment_roles = {value.role for value in environments
                                     if value.role in prior_environments and value.sha256 != prior_environments[value.role].sha256}
        changed_retained = {identity for identity in retained if identity.role in changed_environment_roles}
        retained = {identity for identity in retained if identity.role not in changed_environment_roles}
        if changed_retained:
            endpoints, nginx = feature_network.allocate(host, entry, topology, profile, operation_id,
                retained=tuple(endpoint for endpoint in previous.resolution.endpoints
                               if UnitIdentity(profile.instance_id, endpoint.role, endpoint.ordinal) in retained | reusable))
            resolution_id = str(uuid.uuid4())
            environments = runtime_config.effective_environments(host, entry, resolution_id, roles, endpoints, values=values)
    # Resolution-independent placeholders are replaced before any record is published.
    from listeners import unix_endpoints
    unix = unix_endpoints(topology, profile)
    seed = RuntimeResolution(resolution_id, "0" * 64, revision or entry["configuration_revision"], roles,
                             endpoints, environments, "0" * 64, "0" * 64, nginx, unix)
    specs = []
    secrets = runtime_config.unit_secret_paths(host, entry, topology, paths=private_paths, profile=profile)
    raw_roles = {role["name"]: role for role in topology.projection()["roles"]}
    for unit, role in expanded.items():
        name = unit.role
        if unit in retained:
            specs.append(previous.specs[unit])
            continue
        paths = services.ServicePaths.for_unit(unit, resolution_id)
        views = services.bundle_views(role, paths, topology, profile.architecture, roots, declaration.artifacts)
        executable_bindings, endpoint_bindings, bundle_bindings = [], [], []
        public_listener_bindings = []
        for binding_name, binding in role.common.bindings:
            if isinstance(binding, ExecutableBinding):
                path, checksum = executable(host, binding.executable, declaration, release_path, roots)
                executable_bindings.append(ResolvedExecutableBinding(binding_name, path, checksum))
            elif isinstance(binding, EndpointBinding):
                endpoint = next((e for e in (*endpoints, *unix) if (e.role, e.ordinal, e.listener) == (binding.role, 1, binding.listener)), None)
                require(endpoint is not None, "service_binding", "Declared endpoint is unallocated.")
                endpoint_bindings.append(ResolvedEndpointBinding(binding_name, endpoint, binding.scheme))
            elif isinstance(binding, PublicListenerReference):
                import edge
                public_listener_bindings.append(edge.resolve_public_listener_binding(topology, profile, binding_name, binding))
            elif isinstance(binding, CopiesBinding):
                continue
            else:
                require(isinstance(binding, BundleDirectoryBinding), "service_binding", "Unknown binding kind.")
                view = next(v for v in views if v.name == binding.mount)
                bundle_bindings.append(ResolvedBundleBinding(binding_name, view.destination, view.artifact_sha256))
        import shared_directories
        bindings = RoleBindings(name, tuple(executable_bindings), tuple(endpoint_bindings), tuple(bundle_bindings),
                                tuple(public_listener_bindings), shared_directories.bindings(topology, profile.instance_id, name),
                                runtime_config.resolve_copies_bindings(host, entry, topology, profile, role, seed, paths=private_paths))
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
            config_sha = services.configuration_digest(role, declaration.inventory, views, secrets[unit])
            draft = UnitSpec(unit, binary, checksum, (), canonical_digest(raw_roles[name]), bindings, config_sha)
            args = [services.resolve_argument(a, role, draft, seed, paths, views, secrets[unit]) for a in role.arguments]
        specs.append(UnitSpec(unit, binary, checksum, tuple(args), canonical_digest(raw_roles[name]), bindings, config_sha))
    deployment = Deployment(profile.instance_id, manifest["version"], manifest["schema"], topology.sha256,
                            canonical_digest(profile.projection()), tuple(specs))
    resolution = replace(seed, deployment_sha256=canonical_digest(record_document(deployment)),
                         bindings_sha256=canonical_digest([record_document(s.bindings) for s in specs]),
                         readiness_sha256=canonical_digest([raw_roles[name].get("readiness", name) for name in roles]))
    inventory = services.UnitInventory(entry["name"], topology, profile, deployment, resolution)
    return inventory, secrets


def save_launch(host, entry, inventory, declaration, roots, secret_paths):
    descriptors = {name.record_key: runtime_config.write_binding_descriptor(host, entry, inventory.deployment,
                   inventory.resolution, spec, candidate=True) for name, spec in inventory.specs.items()}
    launch = {"schema": 1, "topology": inventory.topology.projection(), "profile": inventory.profile.projection(),
              "deployment": record_document(inventory.deployment), "resolution": record_document(inventory.resolution),
              "declaration": declaration.projection(), "artifact_roots": roots, "secret_paths": {identity.record_key: paths for identity, paths in secret_paths.items()},
              "descriptors": descriptors}
    path = host.paths.opt(entry["name"]) / "runtime" / inventory.resolution.id / "launch.json"
    if path.exists():
        require(read_json(path) == launch, "runtime_resolution_conflict", "Immutable launch record already differs.")
    host.owned_write(path, json.dumps(launch, sort_keys=True) + "\n", mode=0o640, group=entry["name"])
    return launch


def prepare_shared_directories(host, entry, inventory):
    import shared_directories
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    with store._lock("shared-directories"):
        shared_directories.prepare(host, inventory.topology, inventory.profile.instance_id,
                                   entry["allocations"]["uid"], entry["allocations"]["gid"])


def install_units(host, registry, entry, inventory, launch, tools_dir):
    import provision
    import upgrades
    checksum = upgrades.tool_bundle(tools_dir)[2]
    require(registry["tools"]["bundle_sha256"] == checksum, "host_upgrade_required", "Install the selected preview tools first.")
    operations.install_runtime(host, tools_dir)
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    from release import runtime_declaration
    provision._unit_checks(host, entry)
    prepare_shared_directories(host, entry, inventory)
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
        role = source.roles[name.role]
        paths = services.ServicePaths.for_unit(spec.identity, source.resolution.id)
        views = services.bundle_views(role, paths, source.topology, source.profile.architecture,
                                      source_launch["artifact_roots"], declaration.artifacts)
        runtime_config.prepare_unit_paths(host, role, spec, source.resolution, views=views,
                                          socket_account=(entry["allocations"]["uid"], entry["allocations"]["gid"]))
        content = services.render_unit(source, name, entry, checksum, views=views,
                                       secret_paths=source_launch["secret_paths"].get(name.record_key, {}),
                                       dependency_units=tuple(inventory.units[value] for value in inventory.dependencies(name)))
        provision._owned_write(host, host.paths.p("/etc/systemd/system/" + inventory.units[name]), content)
    http = inventory.identity("http")
    spec = inventory.specs[http]
    command = services.command_line(["/usr/bin/python3", "-I", services.RUNTIME_ENTRY, "boot", spec.identity.instance_id,
                                    "http", "1", generations[http]["resolution_id"], checksum, canonical_digest(record_document(spec))])
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
    names = set(inventory.units) if names is None else set(names)
    for name in inventory.stop_order(names):
        unit = inventory.units[name]
        # StopUnit fails for a candidate that has never been installed. Query
        # that case explicitly; never interpret a failed stop as proof of exit.
        if services.unit_state(host, unit)["LoadState"] == "not-found":
            try:
                services.stopped_unit(host, unit)
            except (DeployError, OSError):
                pass  # A removed unit file does not imply the service has exited.
            else:
                continue
        host.run(["systemctl", "stop", unit], timeout=100)
        services.stopped_unit(host, unit)


def start_units(host, entry, inventory, names, deadline):
    """Takeover/lifecycle interface: start an explicit enabled graph without enabling it."""
    names = set(names)
    enabled = services.enabled_units(host, inventory.units)
    require(names <= set(enabled), "service_enablement", "Cannot start a deliberately disabled role.")
    inventory.start_order(enabled)
    if names:
        prepare_shared_directories(host, entry, inventory)
    prerequisites = inventory.prerequisites(names) - names
    require(not prerequisites or services.wait_ready_set(host, inventory, prerequisites, deadline),
            "service_prerequisite", "A retained prerequisite is unavailable.")
    def start(level, until):
        selected = tuple(name for name in level if name in names)
        if not selected:
            return
        try:
            host.run(["systemctl", "start", *(inventory.units[name] for name in selected)],
                     timeout=max(.001, until - time.monotonic()))
        except DeployError:
            for name in selected:
                _require_namespace_ready(host, inventory.units[name])
            raise
    def await_ready(level, until):
        selected = tuple(name for name in level if name in names)
        if selected and not services.wait_ready_set(host, inventory, selected, until):
            # systemd 255 can report a skipped ExecCondition as a successful start.
            for name in selected:
                _require_namespace_ready(host, inventory.units[name])
            return False
        return True
    inventory.start_in_order(enabled, deadline, start, await_ready)


def _require_namespace_ready(host, unit):
    observed = host.run(["systemctl", "show", unit, "--property=ExecMainStatus,ExecCondition,ExecStartPre", "--no-pager"],
                        check=False, timeout=5)
    if observed.returncode == 0 and re.search(r"(?m)^ExecMainStatus=226$|status=226(?:/NAMESPACE)?(?:[ ;}]|$)", observed.stdout):
        raise DeployError("service_namespace", "Unit namespace setup failed (226/NAMESPACE). "
                          "Run the deploy action again to prepare host paths, or use reboot recovery.")


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
    return set(unit_selection_identities(entry["profile"]["instance_id"], held["running_roles"]))


def transition_enablement(host, entry, inventory, *, restoring=False):
    import release
    if "deployment_selection" not in entry:
        return tuple(inventory.units), tuple(inventory.units), {}
    _, _, previous, _ = load(host, entry)
    enabled = set(services.enabled_units(host, previous.units))
    from copy_enablement import removed_enablement
    removed = removed_enablement(host, entry)
    desired = {name for name in inventory.units if name in enabled or name not in previous.units and
               (removed.get(name, False) if restoring or name in removed else True)}
    held_running = configuration_hold_running(host, entry)
    running = {name for name in desired if name not in previous.units or
               (name in held_running if held_running is not None else
                release.service(host, previous.units[name])["ActiveState"] == "active")}
    while any(inventory.prerequisites({name}) - desired for name in desired):
        desired = {name for name in desired if inventory.prerequisites({name}) <= desired}
    running &= desired
    while any(inventory.prerequisites({name}) - running for name in running):
        running = {name for name in running if inventory.prerequisites({name}) <= running}
    inventory.start_order(desired)
    inventory.start_order(running)
    return tuple(sorted(desired)), tuple(sorted(running)), {name: name in enabled for name in previous.units if name not in inventory.units}



def remove_copy_runtime(host, identity):
    """Drop proven-stopped invocation views, never the copy's persistent state."""
    root = admission.protected_path(host, host.paths.p("/run/foundry/services/" + identity.instance_id), "directory")
    if not root.exists():
        return
    resolutions = [path for path in root.iterdir() if path.name not in ("shared", "sockets")]
    require(len(resolutions) <= 256, "service_history", "Runtime resolution history exceeds its bound.")
    for resolution in resolutions:
        identifier = contract_uuid(resolution.name)
        admission.protected_path(host, resolution, "directory")
        paths = services.ServicePaths.for_unit(identity, identifier)
        base = admission.protected_path(host, host.paths.p(paths.base), "directory")
        if base.exists():
            shutil.rmtree(base)


def named_selection(host, entry, inventory, selection, action, request):
    import release
    selected = inventory.select(request.get("role", "all"), ordinal=request.get("copy"))
    enabled = set(services.enabled_units(host, inventory.units))
    holds = feature_maintenance.active_holds(host, entry, inventory)
    deadline = time.monotonic() + release.READINESS_SECONDS
    prerequisites = inventory.prerequisites(selected) - set(selected) if action == "restart" else set()
    states = {identity: services.RoleState(identity in enabled,
        release.service(host, unit)["ActiveState"] == "active", identity in holds,
        selection.admission.disposition is not AdmissionDisposition.COMMITTED, False)
        for identity, unit in inventory.units.items()}
    observed = {identity for identity in prerequisites if states[identity].enabled and states[identity].running}
    if observed and services.wait_ready_set(host, inventory, observed, deadline):
        states = {identity: replace(state, ready=identity in observed) for identity, state in states.items()}
    return set(inventory.named_operation(action, selected, states))


def transition(host, registry, entry, inventory, state, attempt, *, stop_only=False,
               names=None, configuration=None, launch=None, review=None, maintenance_request=None):
    import release
    from upgrades import check_feature_generation
    minimum_generation = check_feature_generation(registry, inventory.topology, entry, profile=inventory.profile)
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    old = entry.get("deployment_selection", {}).get("selected_resolution_id", inventory.resolution.id)
    previous = load(host, entry)[2] if "deployment_selection" in entry else inventory
    named = names is not None and attempt["action"] in ("stop", "restart") and "role" in attempt
    names = set(names) if names is not None else set(previous.units) | set(inventory.units)
    discard_shared = (previous.deployment.release != inventory.deployment.release and
                      set(previous.units) <= names or
                      previous.topology.shared_directories != inventory.topology.shared_directories)
    require(not discard_shared or not previous.topology.shared_directories or set(previous.units) <= names,
            "shared_directory_transition", "Changing shared caches requires a full instance transition.")
    retained = set(inventory.units) - names
    if review is not None:
        enabled, running, removed = transition_enablement(host, entry, inventory, restoring=review["restoring"])
        require(list(unit_selection_names(enabled)) == review["enabled_roles"] and list(unit_selection_names(running)) == review["running_roles"],
                "stale_plan", "Enablement or running selection changed after review.")
    else:
        enabled = services.enabled_units(host, previous.units) if "deployment_selection" in entry else tuple(inventory.units)
        running, removed = set(enabled), {}
        if named or attempt["action"] == "config" or maintenance_request:
            held_running = configuration_hold_running(host, entry)
            if held_running is not None:
                running = held_running & set(enabled)
            else:
                running = {name for name in enabled if release.service(host, previous.units[name])["ActiveState"] == "active"}
    if named:
        if stop_only:
            enabled, running = set(enabled) - names, set(running) - names
        else:
            enabled, running = set(enabled) | names, set(running) | names
        enabled = tuple(identity for identity in inventory.units if identity in enabled)
    inventory.start_order(enabled)
    holds, running, maintenance_intent = feature_maintenance.selection(
        host, entry, inventory, enabled, running, maintenance_request["action"] if maintenance_request else None,
        allow_start=not (maintenance_request and maintenance_request.get("maintenance_no_start")))
    maintenance_intent["operation_id"] = attempt["operation_id"]
    running = set(running)
    if review is not None:
        require(sorted(unit_selection_names(holds)) == review["maintenance_holds"]
                and sorted(unit_selection_names(running)) == review["maintenance_running_roles"],
                "stale_plan", "Maintenance holds or restored running roles changed after review.")
    inventory.start_order(running)
    if review is not None:
        require(all(name in names or name in running or
                    release.service(host, unit)["ActiveState"] == "inactive"
                    for name, unit in previous.units.items()),
                "stale_plan", "A running unit outside the stop scope would be excluded from the target selection.")
    attempt["running_roles"] = sorted(unit_selection_names(running))
    host.save_state(entry, state)
    selected = DeploymentSelection(inventory.profile.instance_id, minimum_generation, tuple(sorted(FEATURE_CAPABILITIES)),
                 inventory.resolution.id, None if old == inventory.resolution.id else old,
                 AdmissionRecord(inventory.profile.instance_id, inventory.resolution.id, inventory.resolution.deployment_sha256,
                                 inventory.resolution.private_revision, attempt["operation_id"], AdmissionDisposition.PENDING),
                 unit_selection_names(enabled), tuple(sorted(unit_selection_names(holds))))
    request = transition_request(entry, inventory, attempt["operation_id"], names=names, previous=previous)
    if retained:
        store.retain_units(previous, inventory, retained, attempt["operation_id"])
    operations.bind_transition(host, entry, inventory, request)
    store.record_operation(attempt)
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
    stop_units(host, previous, names & previous.units.keys())
    if discard_shared:
        import shared_directories
        shared_directories.discard(host, previous.topology, inventory.profile.instance_id,
                                   entry["allocations"]["uid"], entry["allocations"]["gid"])
    evidence = feature_network.close(host, entry, request, inventory.resolution, units)
    store.record_boot_closure(evidence)
    store._write(store.durable / "transition.json", replace(request, closure_id=evidence.id))
    atomic_json(operations.directory(host, attempt["operation_id"]) / "validated.json", {"operation_id": attempt["operation_id"]}, mode=0o600)
    retained_holds = unit_selection_identities(inventory.profile.instance_id, maintenance_intent["holds"])
    feature_maintenance.clear_markers(host, inventory, retained_holds)
    feature_maintenance.clear_markers(host, previous, retained_holds)
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
    for name in previous.stop_order(set(previous.units) - set(inventory.units)):
        require(name in evidence.units, "admission_closed_required", "Removed copy lacks complete closure evidence.")
        record_removed_copy_enablement(host, entry, name, removed[name])
        host.run(["systemctl", "disable", previous.units[name]])
        path = admission.protected_path(host, host.paths.p("/etc/systemd/system/" + previous.units[name]), "file")
        if path.exists():
            require(marker(entry) in path.read_text(), "unit_ownership", "Removed unit differs from its owned instance.")
            path.unlink()
        remove_copy_runtime(host, name)
    if removed:
        host.run(["systemctl", "daemon-reload"])
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
        stopped = names if named else set(inventory.units)
        host.run(["systemctl", "disable", *(inventory.units[name] for name in inventory.units if name in stopped)])
        enabled = tuple(name for name in enabled if name not in stopped)
        selected = replace(selected, enabled_roles=unit_selection_names(enabled))
        store._write(store.durable / "selection.json", selected)
    else:
        if enabled:
            host.run(["systemctl", "enable", *(inventory.units[name] for name in enabled)])
        for name in set(inventory.units) - set(enabled):
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
        start_units(host, entry, inventory, starting, deadline)
        require(not running or services.wait_ready_set(host, inventory, running, deadline),
                "readiness_failed", "The selected running set did not remain ready before public opening.")
        if running:
            feature_network.open_public(host, entry, inventory.resolution, evidence)
    missing_enablement = review is not None and any(
        copy["evidence"] == "missing" for copy in review.get("restored_copies", ()))
    state["status"] = "degraded" if missing_enablement else "stopped" if not enabled else (
        "healthy" if running | holds == set(inventory.units) else "degraded")
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
    """Rebuild closure and host paths before workloads; never replay pending operations."""
    with host.lock("host"), host.lock(inventory.profile.instance_id):
        store = admission.AdmissionStore(host, inventory.profile.instance_id)
        selected, resolution = store._selection()
        request = HostTransition(selected.admission.operation_id, inventory.profile.instance_id, resolution.id, resolution.id,
                                 resolution.private_revision, tuple(s.identity for s in inventory.specs.values()),
                                 canonical_digest(record_document(inventory.deployment)), None)
        evidence = feature_network.close(host, entry, request, resolution, inventory.units)
        store.record_boot_closure(evidence)
        prepare_shared_directories(host, entry, inventory)
        origins = store._retained_records(resolution)
        for name, spec in inventory.specs.items():
            origin = origins.get(name, {}).get("resolution_id", resolution.id)
            source, launch = load_resolution(host, entry, origin)
            from release import runtime_declaration
            declaration = runtime_declaration(launch["declaration"])
            role = source.roles[name.role]
            paths = services.ServicePaths.for_unit(spec.identity, origin)
            views = services.bundle_views(role, paths, source.topology, source.profile.architecture,
                                          launch["artifact_roots"], declaration.artifacts)
            runtime_config.prepare_unit_paths(host, role, spec, source.resolution, views=views,
                                          socket_account=(entry["allocations"]["uid"], entry["allocations"]["gid"]))


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
        enabled = tuple(sorted(set(services.enabled_units(host, inventory.units)) - set(unit_selection_identities(inventory.profile.instance_id, selection.maintenance_holds))))
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
    paths = runtime_config.unit_secret_paths(host, entry, previous.topology, paths=record["paths"], profile=previous.profile)
    affected = {name for name in previous.units if paths.get(name, {}) != prior_launch["secret_paths"].get(name.record_key, {})}
    if record["application_keys"] or (admission.AdmissionStore(host, previous.profile.instance_id).durable / "configuration-hold.json").exists():
        affected.update(previous.units)
    require(affected, "invalid_configuration", "Configuration intent has no changed effective inputs.")
    inventory, paths = resolve(host, entry, manifest, declaration, path, prior_launch["artifact_roots"], request["operation_id"],
        private_paths=record["paths"], values=values, revision=record["revision"], previous=previous, affected=affected,
        profile=previous.profile.projection())
    launch = save_launch(host, entry, inventory, declaration, prior_launch["artifact_roots"], paths)
    env = runtime_config.effective_environment_path(host, entry, inventory.resolution, candidate=True)
    release.app(host, entry, path, "config:check", env_file=env)
    if record["application_keys"]:
        release.require_configuration_keys(host, entry, path, env, record["application_keys"])
    affected = {name for name in inventory.units if inventory.specs[name] != previous.specs[name]
                or next(e.sha256 for e in inventory.resolution.environments if e.role == name.role) !=
                   next(e.sha256 for e in previous.resolution.environments if e.role == name.role)}
    if (admission.AdmissionStore(host, previous.profile.instance_id).durable / "configuration-hold.json").exists():
        affected = set(inventory.units)
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
                    held = feature_maintenance.active_holds(host, entry, inventory)
                    enabled = tuple(name for name in services.enabled_units(host, inventory.units) if name not in held)
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
                if request.get("role", "all") != "all" or request.get("copy") is not None:
                    names = named_selection(host, entry, inventory, selected, action, request)
                    previous = inventory
                    declaration = release.runtime_declaration(launch["declaration"])
                    inventory, secrets = resolve(host, entry, state["current"], declaration,
                        release.release_path(host, entry, inventory.deployment.release), launch["artifact_roots"],
                        request["operation_id"], previous=previous, affected=names, profile=inventory.profile.projection(), preserve_private=True)
                    launch = save_launch(host, entry, inventory, declaration, launch["artifact_roots"], secrets)
                    # Binding consumers cannot be restarted outside the reviewed named scope.
                    require(changed_units(previous, inventory) <= names,
                            "service_dependents", "Include affected binding consumers; use plain restart/stop.")
                attempt = release.begin(host, entry, state, "restart" if action == "restart" else "stop", inventory.deployment.release,
                                        metadata={"operation_id": request["operation_id"], **({"role": request["role"],
                                                  **({"copy": request["copy"]} if request.get("copy") is not None else {})} if names is not None else {})})
            else:
                import feature_plan
                manifest, inventory, launch, configuration, review = feature_plan.reviewed(host, registry, entry, request, pin=pin)
                names = set(unit_selection_identities(inventory.profile.instance_id, review["unit_changes"]))
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
                       configuration=request.get("configuration_intent"), launch=launch if names is not None or action in ("activate", "rollback", "config-set", "down", "up") else None, review=review,
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
