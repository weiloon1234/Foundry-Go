"""Generation-12 preview lifecycle. Declarations and app bindings are stable.

Recovery/rollback, mutable configuration, policy-aware maintenance and helper pruning
remain explicit capability refusals. Network effects belong exclusively to lane C.
"""
from dataclasses import asdict, replace
import json
import os
from pathlib import Path
import time
import uuid

import admission
import artifacts
import feature_network
import packages
import operations
import runtime_config
import services
from contracts import (AdmissionDisposition, AdmissionRecord, Deployment,
                       DeploymentSelection, DeployError, FEATURE_CAPABILITIES, HostTransition,
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
    if action in ("down", "up"):
        refuse("policy-aware maintenance down/up with retained-role closure and admission")
    require(action in ("activate", "restart", "stop"), "capability_unavailable", f"Feature operation requires {LATER}.")


def validate_preview(topology):
    for role in topology.roles:
        require(not isinstance(role, ExecutableRole) or not role.secret_files,
                "capability_unavailable", f"Custom service secret intake requires {LATER}.")


def absolute(host, path):
    return "/" + Path(path).relative_to(host.paths.root).as_posix()


def load(host, entry):
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    selection, resolution = store._selection()
    path = host.paths.opt(entry["name"]) / "runtime" / resolution.id / "launch.json"
    launch = admission.read_owned(host, path, lambda value: value)
    topology = parse_topology(launch["topology"])
    inventory = services.UnitInventory(entry["name"], topology, parse_profile(launch["profile"], topology),
                                       Deployment.parse(launch["deployment"]), resolution)
    return store, selection, inventory, launch


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


def check_activation(host, entry, state, manifest):
    version = manifest["version"]
    current = (state.get("current") or {}).get("version")
    if version != current and version in selected_releases(host, entry, state):
        refuse("rollback")


def record_selection(host, entry, state, version):
    versions = selected_releases(host, entry, state) | {version}
    path = admission.AdmissionStore(host, entry["profile"]["instance_id"]).durable / "selected-releases.json"
    admission.protected_directory(host, path.parent, 0o700)
    atomic_json(path, sorted(versions), mode=0o600)


def transition_request(entry, inventory, operation):
    old = entry.get("deployment_selection", {}).get("selected_resolution_id", inventory.resolution.id)
    return HostTransition(operation, inventory.profile.instance_id, old, inventory.resolution.id,
                          entry["configuration_revision"], tuple(s.identity for s in inventory.specs.values()),
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


def resolve(host, entry, manifest, declaration, release_path, roots, operation_id):
    topology = parse_topology(declaration.topology_json)
    profile = parse_profile(declaration.profile_json, topology)
    endpoints, nginx = feature_network.allocate(host, entry, topology, profile, operation_id)
    resolution_id = str(uuid.uuid4())
    roles = tuple(role.common.name for role in topology.roles if dict(profile.placement)[role.common.name])
    environments = runtime_config.effective_environments(host, entry, resolution_id, roles, endpoints)
    # Resolution-independent placeholders are replaced before any record is published.
    seed = RuntimeResolution(resolution_id, "0" * 64, entry["configuration_revision"], roles,
                             endpoints, environments, "0" * 64, "0" * 64, nginx)
    specs, secrets = [], {}
    raw_roles = {role["name"]: role for role in topology.projection()["roles"]}
    for role in topology.roles:
        name = role.common.name
        if name not in roles:
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
            args = [command]
            if name == "worker":
                import release
                args += ["--connection", release.worker_connection(host, entry, release_path)]
            args += ["--config", absolute(host, release_path / "config/deploy.toml"), "--env-file",
                     absolute(host, host.paths.instance_config(entry["name"]) / next(e.path for e in environments if e.role == name))]
            config_sha = canonical_digest({"config": manifest["files"]["config/deploy.toml"], "revision": seed.private_revision,
                                           "environment_path": next(e.path for e in environments if e.role == name)})
        else:
            binary, checksum = executable(host, role.executable, declaration, release_path, roots)
            require(not role.secret_files, "capability_unavailable", f"Custom service secret intake requires {LATER}.")
            secrets[name] = {}
            config_sha = services.configuration_digest(role, declaration.inventory, views, secrets[name])
            draft = UnitSpec(unit, binary, checksum, (), canonical_digest(raw_roles[name]), bindings, config_sha)
            args = [services.resolve_argument(a, role, draft, seed, paths, views, secrets[name]) for a in role.arguments]
        specs.append(UnitSpec(unit, binary, checksum, tuple(args), canonical_digest(raw_roles[name]), bindings, config_sha))
    deployment = Deployment(profile.instance_id, manifest["version"], 2, topology.sha256,
                            canonical_digest(profile.projection()), tuple(specs))
    resolution = replace(seed, deployment_sha256=canonical_digest(record_document(deployment)),
                         bindings_sha256=canonical_digest([record_document(s.bindings) for s in specs]),
                         readiness_sha256=canonical_digest([raw_roles[name].get("readiness", name) for name in roles]))
    inventory = services.UnitInventory(entry["name"], topology, profile, deployment, resolution)
    return inventory, secrets


def save_launch(host, entry, inventory, declaration, roots, secret_paths):
    descriptors = {name: runtime_config.write_binding_descriptor(host, entry, inventory.deployment,
                   inventory.resolution, spec) for name, spec in inventory.specs.items()}
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
    declaration = ReleaseDeclaration.parse(launch["declaration"])
    provision._unit_checks(host, entry)
    for name, spec in inventory.specs.items():
        role = inventory.roles[name]
        paths = services.ServicePaths.for_unit(spec.identity, inventory.resolution.id)
        views = services.bundle_views(role, paths, inventory.topology, inventory.profile.architecture,
                                      launch["artifact_roots"], declaration.artifacts)
        runtime_config.prepare_unit_paths(host, role, spec, inventory.resolution, views=views)
        content = services.render_unit(inventory, name, entry, checksum, views=views,
                                       secret_paths=launch["secret_paths"].get(name, {}))
        generation = store.durable / "generations" / (canonical_digest(record_document(spec)) + "-" + checksum + ".json")
        admission.protected_directory(host, generation.parent, 0o700)
        atomic_json(generation, {"unit": asdict(spec.identity), "resolution_id": inventory.resolution.id,
                    "launch_file": entry["allocations"]["paths"]["application"] + "/runtime/" + inventory.resolution.id + "/launch.json"}, mode=0o600)
        provision._owned_write(host, host.paths.p("/etc/systemd/system/" + inventory.units[name]), content)
    spec = inventory.specs["http"]
    command = services.command_line(["/usr/bin/python3", "-I", services.RUNTIME_ENTRY, "boot", spec.identity.instance_id,
                                    "http", "1", inventory.resolution.id, checksum, canonical_digest(record_document(spec))])
    content = (Path(tools_dir) / "server/templates/admission.service").read_text()
    content = content.replace("@MARKER@", marker(entry)).replace("@BOOT@", command)
    boot_unit = "foundry-admission-" + spec.identity.instance_id + ".service"
    provision._owned_write(host, host.paths.p("/etc/systemd/system/" + boot_unit), content)
    host.run(["systemctl", "daemon-reload"])
    opener = "foundry-admission-open-" + spec.identity.instance_id + ".service"
    content = content.replace("Description=", "Description=Open ").replace("RemainAfterExit=yes", "RemainAfterExit=no")
    content = content.replace('"boot"', '"open"').replace("After=network-online.target ufw.service", "After=" + boot_unit)
    provision._owned_write(host, host.paths.p("/etc/systemd/system/" + opener), content)
    host.run(["systemctl", "daemon-reload"])
    host.run(["systemctl", "enable", boot_unit, opener])


def stop_units(host, inventory, names=None):
    names = set(inventory.roles) if names is None else set(names)
    for name in inventory.stop_order(names):
        unit = inventory.units[name]
        host.run(["systemctl", "stop", unit], timeout=100)
        observed = host.run(["systemctl", "show", unit, "--property=MainPID,ControlPID,ControlGroup,ActiveState,Job", "--no-pager"])
        values = dict(line.split("=", 1) for line in observed.stdout.splitlines() if "=" in line)
        require(values.get("MainPID") == "0" and values.get("ControlPID") == "0" and
                values.get("ActiveState") in ("inactive", "failed") and values.get("Job") in ("", "0"),
                "feature_stop_unverified", "Selected unit exit remains unverified.")
        group = values.get("ControlGroup", "")
        if group:
            path = host.paths.p("/sys/fs/cgroup" + group)
            require(path.is_dir() and all(not file.read_text().strip() for file in path.rglob("cgroup.procs")),
                    "feature_stop_unverified", "Selected unit still owns workload processes.")


def ready(host, entry, inventory, name, deadline):
    return services.wait_ready(host, inventory, name, deadline)


def current_executor(host):
    invocation = os.environ.get("INVOCATION_ID", "")
    stat = host.paths.p(f"/proc/{os.getpid()}/stat").read_text().rsplit(") ", 1)[1].split()
    return ProcessIdentity.parse({"pid": os.getpid(), "start_ticks": int(stat[19]), "invocation_id": invocation})


def transition(host, registry, entry, inventory, state, attempt, *, stop_only=False):
    import release
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    old = entry.get("deployment_selection", {}).get("selected_resolution_id", inventory.resolution.id)
    selected = DeploymentSelection(inventory.profile.instance_id, 12, tuple(sorted(FEATURE_CAPABILITIES)),
                 inventory.resolution.id, None if old == inventory.resolution.id else old,
                 AdmissionRecord(inventory.profile.instance_id, inventory.resolution.id, inventory.resolution.deployment_sha256,
                                 entry["configuration_revision"], attempt["operation_id"], AdmissionDisposition.PENDING),
                 tuple(inventory.roles), ())
    request = transition_request(entry, inventory, attempt["operation_id"])
    operations.bind_transition(host, entry, inventory, request)
    store.record_operation(attempt)
    store.begin(selected, inventory.resolution)
    store._write(store.durable / "transition.json", request)
    stop_units(host, inventory)
    evidence = feature_network.close(host, entry, request, inventory.resolution, inventory.units)
    store.record_boot_closure(evidence)
    store._write(store.durable / "transition.json", replace(request, closure_id=evidence.id))
    http = next(e for e in inventory.resolution.endpoints if e.role == "http" and e.listener == "http")
    entry["deployment_selection"] = record_document(selected)
    entry["allocations"]["http_port"] = http.port
    host.journal(registry, entry)
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
    if stop_only:
        host.run(["systemctl", "disable", *inventory.units.values()])
        selected = replace(selected, enabled_roles=())
        store._write(store.durable / "selection.json", selected)
    else:
        attempt.update(phase="health")
        host.save_state(entry, state)
        store.record_operation(attempt)
        permit = ValidationPermit(str(uuid.uuid4()), attempt["operation_id"], admission.boot_id(host), inventory.resolution.id,
                                  evidence.id, current_executor(host), tuple(s.identity for s in inventory.specs.values()))
        store.issue_permit(permit, evidence)
        atomic_write(store.runtime / "active-permit", permit.id, mode=0o600)
        deadline = time.monotonic() + release.READINESS_SECONDS
        inventory.start_in_order(inventory.roles, deadline,
            lambda name, until: host.run(["systemctl", "start", inventory.units[name]], timeout=max(.001, until - time.monotonic())),
            lambda name, until: ready(host, entry, inventory, name, until))
        # The boot coordinator starts the enabled selection in readiness order.
        # Workload units themselves must not race that coordinator at boot.
        host.run(["systemctl", "disable", *inventory.units.values()])
        require(services.wait_ready_set(host, inventory, selected.enabled_roles, deadline),
                "readiness_failed", "The complete enabled set did not remain ready before public opening.")
        feature_network.open_public(host, entry, inventory.resolution, evidence)
    state["status"] = "stopped" if stop_only else "healthy"
    release.finish(host, entry, state, "succeeded")
    store.record_operation(state["last_attempt"])
    store.commit()
    entry["deployment_selection"] = record_document(store._selection()[0])
    host.journal(registry, entry)


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
        _, _, _, launch = load(host, entry)
        declaration = ReleaseDeclaration.parse(launch["declaration"])
        for name, spec in inventory.specs.items():
            role = inventory.roles[name]
            paths = services.ServicePaths.for_unit(spec.identity, resolution.id)
            views = services.bundle_views(role, paths, inventory.topology, inventory.profile.architecture,
                                          launch["artifact_roots"], declaration.artifacts)
            runtime_config.prepare_unit_paths(host, role, spec, resolution, views=views)


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
        if not selection.enabled_roles:
            return
        evidence = admission.read_owned(host, store.runtime / "boot.json", ClosureEvidence.parse)
        deadline = time.monotonic() + release.READINESS_SECONDS
        inventory.start_in_order(selection.enabled_roles, deadline,
            lambda name, until: host.run(["systemctl", "start", inventory.units[name]], timeout=max(.001, until - time.monotonic())),
            lambda name, until: ready(host, entry, inventory, name, until))
        require(services.wait_ready_set(host, inventory, selection.enabled_roles, deadline),
                "readiness_failed", "The complete enabled set did not remain ready before boot opening.")
        feature_network.open_public(host, entry, resolution, evidence)


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
        require(is_feature(entry) and registry["tools"]["generation"] >= 12,
                "capability_unavailable", "Feature execution requires the generation-12 preview release.")
        state = host.load_state(entry)
        admission.AdmissionStore(host, profile["instance_id"]).require_cleanup_clear(request["operation_id"])
        release.require_idle(state)
        inventory, attempt = None, None
        try:
            if action != "activate":
                store, selected, inventory, launch = load(host, entry)
                require(request.get("role", "all") == "all", "capability_unavailable", f"Named feature operations require {LATER}.")
                attempt = release.begin(host, entry, state, "restart" if action == "restart" else "stop", inventory.deployment.release,
                                        metadata={"operation_id": request["operation_id"]})
            else:
                archive = check_path(Path(request["archive"]), "file")
                guard = lambda manifest: check_activation(host, entry, state, manifest)
                release.unpack_archive(host, archive, None, entry, request["archive_sha256"], manifest_check=guard)
                stage_root = operations.staging(host, entry, request["operation_id"])
                admission.protected_directory(host, stage_root, 0o700)
                staging = stage_root / "release"
                budget = artifacts.ExpansionBudget()
                manifest = release.unpack_archive(host, archive, staging, entry, request["archive_sha256"],
                                                  expansion_budget=budget, manifest_check=guard)
                declaration = ReleaseDeclaration.parse(manifest["deployment"])
                topology = parse_topology(declaration.topology_json)
                profile_record = parse_profile(declaration.profile_json, topology)
                validate_preview(topology)
                plan = packages.inspect(host, topology, profile_record)
                require(not plan["missing"], "capability_unavailable", f"New package requirements after bootstrap need {LATER}; rebuild this preview host.")
                prepared = artifacts.verify_release_tree(declaration, staging, budget,
                             artifact_staging=stage_root / "artifacts", outer_charged=True)
                store = admission.AdmissionStore(host, profile["instance_id"])
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
                    release.verified_release(host, entry, manifest)
                else:
                    os.rename(staging, release_path)
                    fd = os.open(release_path.parent, os.O_RDONLY | os.O_DIRECTORY)
                    try:
                        os.fsync(fd)
                    finally:
                        os.close(fd)
                attempt = release.begin(host, entry, state, "deploy", manifest["version"], metadata={"operation_id": request["operation_id"]})
                store.record_operation(attempt)
                inventory, secret_paths = resolve(host, entry, manifest, declaration, release_path, roots, request["operation_id"])
                launch = save_launch(host, entry, inventory, declaration, roots, secret_paths)
                attempt.update(candidate_resolution_id=inventory.resolution.id,
                               private_revision=inventory.resolution.private_revision)
                host.save_state(entry, state)
                store.record_operation(attempt)
                env = runtime_config.effective_environment_path(host, entry, inventory.resolution)
                release.native_runtime(host, entry)
                release.protect_maps(host, entry, release_path)
                release.app(host, entry, release_path, "config:check", env_file=env)
                patterns = release.release_redis_inventory(host, entry, release_path, env_file=env)
            store = admission.AdmissionStore(host, profile["instance_id"])
            store.record_operation(attempt)
            operations.bind_transition(host, entry, inventory, transition_request(entry, inventory, attempt["operation_id"]))
            if action == "activate":
                release.phase(host, entry, state, "redis")
                store.record_operation(attempt)
                release.widen_redis_grants(host, registry, entry, patterns, state)
                install_units(host, registry, entry, inventory, launch, Path(__file__).resolve().parents[1])
                state["previous"], state["current"] = state["current"], manifest
            transition(host, registry, entry, inventory, state, attempt, stop_only=action == "stop")
        except (DeployError, OSError):
            if attempt is None:
                raise
            state["status"] = "unknown"
            release.finish(host, entry, state, "needs_reconciliation", "feature_transition_pending")
            store = admission.AdmissionStore(host, profile["instance_id"])
            store.record_operation(state["last_attempt"])
            # ExecStopPost owns closure independently, including SIGKILL paths.
            raise
        return {"ok": True, "instance": entry["name"], "version": inventory.deployment.release,
                "status": state["status"], "preview": True,
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
            require(read_json(path) == request, "operation_identity", "Operation UUID belongs to another request.")
            if (directory / "result.json").exists():
                admission.AdmissionStore(host, profile["instance_id"]).require_cleanup_clear()
                receipt = read_json(directory / "result.json")
                if "error" in receipt:
                    raise DeployError(receipt["error"]["code"], receipt["error"]["detail"])
                return receipt
            refuse("interrupted-operation reconciliation")
        store = admission.AdmissionStore(host, profile["instance_id"])
        store.require_cleanup_clear()
        if action == "activate":
            import release
            state = host.load_state(entry)
            release.require_idle(state)
            release.unpack_archive(host, check_path(Path(request["archive"]), "file"), None, entry,
                                   request["archive_sha256"], manifest_check=lambda m: check_activation(host, entry, state, m))
        owner = operations.prepare(host, registry, entry, request)
    unit = "foundry-operation-" + operation_id
    cleanup = services.command_line(operations.command("operation-cleanup", operation_id, owner["bundle_sha256"]))
    result = host.run(["systemd-run", "--quiet", "--wait", "--collect", "--unit", unit,
              "--property=Type=exec", "--property=Restart=no", "--property=KillMode=control-group",
              "--property=TimeoutStopSec=3600", "--property=RuntimeMaxSec=1800", "--property=ExecStopPost=" + cleanup,
              *operations.command("operation", operation_id, owner["bundle_sha256"])], timeout=5400, check=False)
    receipt = admission.read_owned(host, directory / "result.json", lambda value: value)
    if "error" in receipt:
        raise DeployError(receipt["error"]["code"], receipt["error"]["detail"])
    store.require_cleanup_clear()
    require(result.returncode == 0, "feature_operation_failed", "Operation exit differs from its receipt; inspect the journal.")
    return receipt
