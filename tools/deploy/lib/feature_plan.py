"""Private release preparation and public, digest-bound review before admission changes."""
from pathlib import Path
from dataclasses import asdict

import admission
import feature_lifecycle as lifecycle
import feature_network
import listeners
import operations
import packages
import runtime_config
import services
from contracts import (Deployment, ReleaseDeclaration, RuntimeResolution, atomic_json,
                       canonical_digest, contract_uuid, digest, record_document, read_json, require)
from topology import parse_profile, parse_topology


def observations(host, registry, entry, topology, profile):
    import release
    import feature_maintenance
    package = packages.inspect(host, feature_network.package_topology(topology), profile)
    enabled, units = [], {}
    if "deployment_selection" in entry:
        _, _, current, _ = lifecycle.load(host, entry)
        enabled = list(services.enabled_units(host, current.units))
        units = {name: release.service(host, unit) for name, unit in current.units.items()}
        network = feature_network.plan(host, current.topology, current.profile, package, entry=entry)
    else:
        network = feature_network.plan(host, topology, profile, package, entry=entry)
    return {"host_revision": registry["revision"], "configuration_revision": entry["configuration_revision"],
            "current_release": release.current_version(host, entry),
            "maintenance": feature_maintenance.live(host, entry), "maintenance_hold": feature_maintenance.record(host, entry),
            "current_resolution": entry.get("deployment_selection", {}).get("selected_resolution_id"),
            "packages": package, "network": network, "enabled": enabled, "units": units,
            "dns": [{"hostname": observation.hostname, "addresses": list(observation.addresses)}
                    for _, binding in profile.listeners for observation in [listeners.observe_dns(binding)]]}



def retained_release(host, entry, target, supplied_topology):
    import release
    path = release.release_path(host, entry, target)
    manifest = read_json(path / "manifest.json")
    release.verified_release(host, entry, manifest, reviewed=True)
    recorded = release.target_declaration(host, entry, manifest, supplied_topology)
    require(release.legacy_profile(recorded["profile"]) == release.legacy_profile(entry["profile"]),
            "release_identity", "Target profile belongs to different host infrastructure.")
    if manifest["schema"] == 1:
        return manifest, release.runtime_declaration({"schema": 1, "topology": recorded["topology"],
                                                    "profile": recorded["profile"]}), path, {}, recorded["archive_sha256"]
    declaration = ReleaseDeclaration.parse(manifest["deployment"])
    root = host.paths.opt(entry["name"]) / "runtime"
    launches = list(root.glob("*/launch.json"))
    require(len(launches) <= 256, "release_history", "Retained runtime inventory exceeds its bound.")
    for saved in launches:
        launch = admission.read_owned(host, saved, lambda v: v)
        if launch["deployment"]["release"] == target and launch["declaration"] == declaration.projection():
            return manifest, declaration, path, launch["artifact_roots"], recorded["archive_sha256"]
    require(False, "rollback_artifacts_required", "Target artifact selections are unavailable; upload the original release archive for reviewed reactivation.")

def plan(host, project, profile, archive, checksum, operation_id, configuration=None, *, target=None, expected_current=None, supplied_topology=None):
    import release
    operation_id = contract_uuid(operation_id)
    with host.lock("host"), host.lock(profile["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        lifecycle.migrate_enablement(host, registry, entry)
        path = operations.directory(host, operation_id) / "release-plan.json"
        if path.exists():
            saved = admission.read_owned(host, path, lambda value: value)
            public = saved["public"]
            same = (public["action"] == "rollback" and target == public["release"]
                    and expected_current == public["expected_current"]) if target is not None else (
                    public["action"] == "activate" and checksum == public["archive_sha256"]
                    and digest(Path(archive)) == checksum)
            require(same and not configuration, "operation_identity", "Use a fresh operation UUID for a changed review.")
            if supplied_topology is not None:
                require(parse_topology(supplied_topology).sha256 == parse_topology(saved["launch"]["topology"]).sha256,
                        "release_topology", "Supplied topology differs from the reviewed target.")
            return public
        state = host.load_state(entry)
        release.require_idle(state)
        admission.AdmissionStore(host, profile["instance_id"]).require_cleanup_clear()
        rollback = target is not None
        if rollback:
            require(release.valid_version(target) and release.current_version(host, entry) == expected_current,
                    "stale_current", "Review an explicit rollback target against the current release.")
            require(not configuration and archive is None and checksum is None,
                    "invalid_rollback", "Rollback retains current credentials; private restoration is a separate operation.")
            manifest, declaration, target_path, roots, checksum = retained_release(host, entry, target, supplied_topology)
        else:
            request = {"operation_id": operation_id, "archive": str(archive), "archive_sha256": checksum}
            prepared = path.with_name("prepared-release.json")
            if prepared.exists():
                prior = admission.read_owned(host, prepared, lambda v: v)
                require(prior["archive_sha256"] == checksum and digest(Path(archive)) == checksum,
                        "operation_identity", "Release review UUID belongs to another archive.")
                manifest, roots = prior["manifest"], prior["roots"]
                declaration = ReleaseDeclaration.parse(manifest["deployment"])
                target = release.verified_release(host, entry, manifest, reviewed=True)
            else:
                manifest, declaration, target, roots = lifecycle.prepare_release(host, entry, state, request)
                admission.protected_directory(host, path.parent, 0o700)
                atomic_json(prepared, {"archive_sha256": checksum, "manifest": manifest, "roots": roots}, mode=0o600)
        if not rollback:
            target_path = target
        topology = parse_topology(declaration.topology_json)
        candidate_profile = parse_profile(declaration.profile_json, topology)
        import feature_maintenance
        feature_maintenance.stopped_roles(topology)
        application, changes = runtime_config.configuration_parts(configuration or {}, topology=topology)
        require(not application, "invalid_configuration", "Release review accepts only declared service secrets; use config for application changes.")
        paths = runtime_config.stage_service_secrets(host, entry, topology, changes, required=False)
        requirements = runtime_config.service_secret_requirements(topology, candidate_profile, paths)
        if any(not r["present"] for r in requirements):
            require(not rollback, "rollback_secret_required", "Target requires a secret absent from current configuration; rotate configuration explicitly before rollback.")
            return {"ok": False, "operation_id": operation_id, "required_service_secrets": requirements}
        intent = runtime_config.stage_service_secret_intent(host, entry, topology, candidate_profile, changes)
        paths = runtime_config.read_service_secret_intent(host, entry, intent, topology, candidate_profile)
        configuration_intent = None
        if paths != runtime_config.service_secret_snapshot(host, entry):
            configuration_intent = runtime_config.stage_configuration_revision(host, entry, changes, topology, candidate_profile)
            config, values = runtime_config.read_configuration_revision(host, entry, configuration_intent)
            paths = config["paths"]
        else:
            values = runtime_config.validate_runtime_environment(host, entry)
        inventory, secret_paths = lifecycle.resolve(host, entry, manifest, declaration, target_path, roots, operation_id,
            private_paths=paths, values=values,
            revision=entry["configuration_revision"] + bool(configuration_intent))
        launch = lifecycle.save_launch(host, entry, inventory, declaration, roots, secret_paths)
        env = runtime_config.effective_environment_path(host, entry, inventory.resolution, candidate=True)
        release.app(host, entry, target_path, "config:check", env_file=env)
        restoring = rollback or (manifest["version"] != release.current_version(host, entry) and
                                  manifest["version"] in lifecycle.selected_releases(host, entry, state))
        if restoring:
            require(not release.migration_status(host, entry, target_path, env_file=env),
                    "rollback_schema", "Target release is incompatible with the current database schema.")
        observed = observations(host, registry, entry, topology, candidate_profile)
        enabled, running, removed = lifecycle.transition_enablement(host, entry, inventory, restoring=restoring)
        # Values and credential-derived digests exist only in the private records.
        public = {"ok": True, "operation_id": operation_id, "archive_sha256": checksum,
                  "deployment_sha256": inventory.resolution.deployment_sha256,
                  "candidate_resolution": inventory.resolution.id, "release": manifest["version"],
                  "action": "rollback" if rollback else "activate", "expected_current": expected_current,
                  "private_intent_id": intent, "configuration_intent_id": configuration_intent,
                  "observations": observed, "endpoints": [asdict(e) for e in inventory.resolution.endpoints],
                  "unit_changes": list(inventory.units), "required_service_secrets": requirements,
                  "maintenance_policy": {name: "stop" if role.common.maintenance == "stop" else "continue"
                                         for name, role in inventory.roles.items()},
                  "restoring": restoring, "enabled_roles": list(enabled), "running_roles": list(running),
                  "removed_enablement": removed}
        public["plan_token"] = canonical_digest(public)
        admission.protected_directory(host, path.parent, 0o700)
        atomic_json(path, {"public": public, "manifest": manifest, "launch": launch}, mode=0o600)
        return public


def reviewed(host, registry, entry, request):
    """Reobserve under both locks, before marking pending or withdrawing ingress."""
    path = operations.directory(host, contract_uuid(request["operation_id"])) / "release-plan.json"
    require(path.exists(), "release_plan_required", "Review the release plan before activation.")
    value = admission.read_owned(host, path, lambda v: v)
    public, launch = value["public"], value["launch"]
    require(request.get("plan_token") == public["plan_token"]
            and public["plan_token"] == canonical_digest({k: v for k, v in public.items() if k != "plan_token"})
            and request["action"] == public["action"], "stale_plan", "Release request differs from its reviewed token.")
    if public["action"] == "rollback":
        require(request.get("target") == public["release"] and request.get("expected_current") == public["expected_current"],
                "stale_plan", "Rollback selection differs from review.")
    else:
        require(request.get("archive_sha256") == public["archive_sha256"]
                and digest(Path(request["archive"])) == public["archive_sha256"],
                "stale_plan", "Release archive changed after review.")
    topology = parse_topology(launch["topology"])
    profile = parse_profile(launch["profile"], topology)
    require(observations(host, registry, entry, topology, profile) == public["observations"],
            "stale_plan", "Host, enabled units, network, or configuration changed after release review.")
    runtime_config.read_service_secret_intent(host, entry, public["private_intent_id"], topology, profile)
    configuration = public["configuration_intent_id"]
    if configuration:
        runtime_config.read_configuration_revision(host, entry, configuration)
    inventory = services.UnitInventory(entry["name"], topology, profile,
        Deployment.parse(launch["deployment"]), RuntimeResolution.parse(launch["resolution"]))
    import release
    target = release.verified_release(host, entry, value["manifest"], reviewed=True)
    env = runtime_config.effective_environment_path(host, entry, inventory.resolution, candidate=True)
    release.app(host, entry, target, "config:check", env_file=env)
    if public["restoring"]:
        require(not release.migration_status(host, entry, target, env_file=env), "rollback_schema", "Target database schema is incompatible.")
    return value["manifest"], inventory, launch, configuration, public
