#!/usr/bin/env python3
"""SSH host entrypoint. JSON request on stdin; one safe JSON result on stdout."""
import argparse
from contextlib import ExitStack
import json
import os
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, FEATURE_LIFECYCLE_GENERATION, MAX_JSON, check_path, identity, now, parse_env, parse_json, read_json, require
from topology import instance_units, selected_units
from host import Host
import journal
import maintenance

ACTIONS = ("capacity-plan", "capacity-apply", "hello", "sync", "tools-plan", "tools-apply", "upgrade-resume", "inspect",
           "bootstrap-plan", "bootstrap-apply", "recover-plan", "recover-apply", "release-plan", "activate", "status", "rollback", "restart",
           "stop", "down", "up", "backup-keys-rotate", "backup-recipients-rotate", "dns-token-rotate", "backup-configure", "backup-run",
           "restore-drill", "logs", "config-keys", "config-set", "keys-rotate", "reconcile", "report")


def execute(host, action, request):
    from provision import inspect_host, bootstrap_plan, bootstrap_apply
    from report import collect_report
    import release
    import upgrades

    require(action in ACTIONS, "invalid_action", "Unknown deployment action.")
    require(isinstance(request, dict), "invalid_request", "Request must be a JSON object.")
    require("follow" not in request and (action == "logs" or "lines" not in request),
            "invalid_request", "Only logs accepts lines; follow is local-only.")
    if action == "hello":
        return upgrades.hello(host)
    common = {"protocol", "client", "project", "profile", "operation_id", "confirm"}
    recovery_fields = {"topology", "mode", "recovery_archive", "archive_sha256", "target_time", "recovery_public_binding"}
    arguments = {"recover-plan": recovery_fields,
                 "recover-apply": recovery_fields | {"archive", "plan_token", "old_server_gone"}, "bootstrap-plan": {"topology"}, "bootstrap-apply": {"topology", "plan_token", "secrets"},
                 "restart": {"role"}, "stop": {"role"}, "logs": {"role", "lines"}, "down": maintenance.FIELDS,
                 "activate": {"archive", "archive_sha256", "plan_token"}, "release-plan": {"archive", "archive_sha256", "secrets", "target", "expected_current", "topology"}, "config-set": {"values", "secrets"},
                 "keys-rotate": {"secrets"}, "rollback": {"target", "expected_current", "plan_token"},
                 "backup-keys-rotate": {"secrets"}, "backup-recipients-rotate": {"secrets"},
                 "dns-token-rotate": {"secrets"}, "backup-configure": {"secrets"}, "backup-run": {"kind"},
                 "restore-drill": {"backup_id", "target_time", "target_inclusive", "retain"},
                 "capacity-apply": {"plan_token"}, "tools-apply": {"plan_token"}, "upgrade-resume": {"plan_token"}}
    if action not in upgrades.READ_ACTIONS or action == "logs":
        require(set(request) <= common | arguments.get(action, set()), "invalid_request", "Unknown deployment request field.")
    if action not in upgrades.READ_ACTIONS:
        require(os.geteuid() == 0 or host.paths.root != Path("/"), "root_required", "Host mutations require root.")
    if request.get("profile") is not None:
        registry = host.read_registry(missing=True)
        expected_host = request["profile"].get("host", {}).get("host_id")
        require(not registry or expected_host in (None, registry["host_id"]), "host_mismatch", "Host UUID differs from the bound profile.")
    upgrades.check_client(host, request, action)
    if action in ("tools-plan", "tools-apply", "upgrade-resume"):
        candidate = Path(__file__).resolve().parents[1]
        if action == "tools-plan":
            return upgrades.plan_upgrade(host, candidate)
        return upgrades.apply_upgrade(host, candidate, request.get("plan_token"), request.get("operation_id"))
    if action == "report":
        return collect_report(host)
    require(os.geteuid() == 0 or host.paths.root != Path("/"), "root_required", "Use root or a configured sudo administrator for host operations.")
    if action == "inspect":
        return inspect_host(host)
    project, profile = request.get("project"), request.get("profile")
    identity(project, profile)
    if action in ("recover-plan", "recover-apply"):
        import recover
        arguments = {key: request.get(key) for key in ("topology", "mode", "operation_id", "archive_sha256", "target_time", "recovery_public_binding")}
        require(isinstance(request.get("recovery_archive"), str), "recovery_members", "A staged private recovery archive is required.")
        arguments["recovery_archive"] = request["recovery_archive"]
        source = Path(__file__).resolve().parents[1]
        if action == "recover-plan":
            return recover.public_plan(recover.plan(host, project, profile, source, **arguments))
        require(request.get("confirm") is True, "confirmation_required", "Review and confirm the recovery plan first.")
        require(isinstance(request.get("archive"), str), "invalid_request", "A staged release archive is required.")
        return recover.apply(host, project, profile, source, **arguments, archive=request["archive"],
                             plan_token=request.get("plan_token"), old_server_gone=request.get("old_server_gone", False))
    if action == "bootstrap-plan":
        return bootstrap_plan(host, project, profile, topology=request.get("topology"))
    if action == "bootstrap-apply":
        require(request.get("confirm") is True, "confirmation_required", "Review and confirm the bootstrap plan before applying it.")
        # The apply path replans under its host lock. Its token detects drift
        # between the operator's approved plan and that fresh snapshot.
        private = request.get("secrets", {})
        require(isinstance(private, dict), "invalid_secrets", "Bootstrap secrets must be an object.")
        host.secrets = private
        try:
            return bootstrap_apply(host, project, profile, Path(__file__).resolve().parents[1],
                                   plan_token=request.get("plan_token"), topology=request.get("topology"),
                                   private_secrets=private)
        finally:
            del host.secrets
    if action == "sync":
        registry = host.read_registry()
        require(registry["host_id"] == profile["host"]["host_id"], "host_mismatch", "Host binding differs.")
        entry = registry["instances"].get(profile["instance_id"])
        require(entry is not None and entry["project_id"] == project["id"], "identity_mismatch", "Unknown project/instance UUID.")
        return {"profile": entry["profile"], "topology": entry["topology"]}
    _, entry = host.require_identity(project, profile)
    import feature_lifecycle
    enablement_migration = None
    if feature_lifecycle.is_feature(entry) and request.get("client", {}).get("generation", 0) >= FEATURE_LIFECYCLE_GENERATION:
        with host.lock("host"), host.lock(profile["instance_id"]):
            registry, entry = host.require_identity(project, profile)
            enablement_migration = feature_lifecycle.migrate_enablement(host, registry, entry)
        if action not in ("down", "up", "activate", "rollback"):
            import feature_maintenance
            feature_maintenance.reconcile(host, project, profile)
            _, entry = host.require_identity(project, profile)
    if action == "release-plan":
        if not feature_lifecycle.is_feature(entry):
            require(request.get("target") is not None, "invalid_request", "Legacy release plans select an explicit rollback target.")
            return release.legacy_rollback_plan(host, project, profile, request["target"], request.get("expected_current"),
                                                request.get("operation_id"), request.get("topology"))
        import feature_plan
        private = request.get("secrets", {})
        require(isinstance(private, dict) and set(private) <= {"configuration"}, "invalid_secrets", "Invalid release private input.")
        return feature_plan.plan(host, project, profile, request.get("archive"), request.get("archive_sha256"),
                                 request.get("operation_id"), private.get("configuration"), target=request.get("target"),
                                 expected_current=request.get("expected_current"), supplied_topology=request.get("topology"))
    if feature_lifecycle.is_feature(entry) and action in ("activate", "rollback", "restart", "stop", "down", "up"):
        feature_lifecycle.check_operation(action)
        arguments = {key: request[key] for key in ("archive", "archive_sha256", "plan_token", "role", "target", "expected_current") if key in request}
        if action in ("down", "up"):
            require(request.get("confirm") is True, "confirmation_required", "Confirm the maintenance change.")
            arguments["maintenance"] = {key: request[key] for key in maintenance.FIELDS if key in request}
            maintenance.arguments(action, arguments["maintenance"])
        return feature_lifecycle.request_operation(host, action, project, profile,
                   operation_id=request.get("operation_id"), **arguments)
    if action in ("capacity-plan", "capacity-apply"):
        import capacity
        if action == "capacity-plan":
            return capacity.plan(host)
        require(request.get("confirm") is True, "confirmation_required", "Review and confirm the capacity plan before restarting services.")
        return capacity.apply(host, request.get("plan_token"))
    if action in ("backup-keys-rotate", "backup-recipients-rotate", "dns-token-rotate"):
        import backup
        import certificates
        rotations = {"backup-keys-rotate": ("backup_s3", backup.rotate_keys),
                     "backup-recipients-rotate": ("age_recipients", backup.rotate_recipients),
                     "dns-token-rotate": ("cloudflare_dns_token", certificates.rotate_token)}
        name, rotate = rotations[action]
        require(request.get("confirm") is True, "confirmation_required", "Confirm credential replacement and its shared scope.")
        private = request.get("secrets")
        require(isinstance(private, dict) and set(private) == {name}, "invalid_secrets", "Supply exactly the selected rotation input.")
        with host.lock("host"), ExitStack() as locks:
            registry, entry = host.require_identity(project, profile)
            affected = [entry]
            if action == "backup-keys-rotate":
                affected = list(registry["instances"].values())
            elif action == "dns-token-rotate":
                affected = [item for item in registry["instances"].values()
                            if item["certificate"].get("zone_ref") == entry["certificate"].get("zone_ref")]
            for item in sorted(affected, key=lambda item: item["profile"]["instance_id"]):
                locks.enter_context(host.lock(item["profile"]["instance_id"]))
            for item in affected:
                release.require_idle(host.load_state(item))
            return rotate(host, registry, entry, private[name])
    if action in ("backup-configure", "backup-run", "restore-drill"):
        import backup
        from contracts import valid_uuid
        require(request.get("confirm") is True, "confirmation_required", "Confirm this backup operation.")
        with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
            registry, entry = host.require_identity(project, profile)
            if action == "backup-configure":
                from runtime_config import initialize_bootstrap_secrets
                private = request.get("secrets", {})
                require(isinstance(private, dict) and set(private) <= {"backup_s3", "age_recipients"},
                        "invalid_secrets", "Invalid backup secret envelope.")
                initialize_bootstrap_secrets(host, entry, private)
                backup.configure(host, registry, entry)
                return {"ok": True, "instance": entry["name"]}
            if action == "backup-run":
                require(valid_uuid(request.get("operation_id")), "invalid_operation", "A stable backup UUIDv4 is required.")
                receipt = backup.run_backup(host, entry, request["operation_id"], request.get("kind", "full"))
            else:
                receipt = backup.restore_drill(host, entry, request.get("backup_id"),
                                              target_time=request.get("target_time"),
                                              target_inclusive=request.get("target_inclusive", True), retain=request.get("retain", False))
            return {**receipt, "ok": True}
    if action == "activate":
        require(isinstance(request.get("archive"), str), "invalid_request", "A staged release archive is required.")
        return release.activate(host, project, profile, request["archive"], request.get("archive_sha256"),
                                operation_id=request.get("operation_id"))
    if action == "rollback":
        return release.rollback(host, project, profile, request.get("target"),
                                request.get("expected_current"), request.get("operation_id"), request.get("plan_token"))
    if action == "reconcile":
        require(request.get("confirm") is True, "confirmation_required", "Inspect status and confirm reconciliation first.")
        return release.reconcile(host, project, profile)
    if action == "config-set":
        require(request.get("confirm") is True, "confirmation_required", "Confirm this explicit configuration change.")
        private = request.get("secrets", {})
        require(isinstance(private, dict) and set(private) <= {"configuration"}
                and not ("values" in request and "configuration" in private),
                "invalid_secrets", "Supply one private configuration change set.")
        return release.config_set(host, project, profile, private.get("configuration", request.get("values")))
    if action == "keys-rotate":
        require(request.get("confirm") is True, "confirmation_required", "Confirm encryption key rotation.")
        private = request.get("secrets", {})
        require(isinstance(private, dict) and set(private) <= {"encryption"}, "invalid_secrets", "Invalid rotation secret fields.")
        return release.keys_rotate(host, project, profile, request.get("operation_id"), private.get("encryption"))
    if action == "config-keys":
        values = parse_env(check_path(host.paths.instance_config(entry["name"]) / "app.env", "file").read_text())
        if feature_lifecycle.is_feature(entry):
            from topology import parse_topology, service_secret_keys
            values.update({key: "[redacted]" for key in service_secret_keys(dict(parse_topology(entry["topology"], registered=True).secrets))})
        return {"keys": sorted(values), "locked_keys": sorted(release.pipeline_keys(entry)), "configuration_revision": entry["configuration_revision"]}
    if action == "logs":
        # Explicit operator action. Logs are never included in the host report.
        argv = journal.arguments(entry, request.get("role", "all"), request.get("lines", journal.DEFAULT_LINES))
        result = host.run(argv, timeout=15)
        response = {"instance": entry["name"], "logs": result.stdout}
        # Count JSON escaping and main's trailing newline, including oversized single entries.
        require(len(json.dumps(response, separators=(",", ":")).encode("utf-8")) + 1 <= MAX_JSON,
                "logs_too_large", "Journal snapshot exceeds the JSON size limit; reduce LINES or use FOLLOW=yes.")
        return response
    if action == "status":
        state = release.public_state(host.load_state(entry))
        version = release.current_version(host, entry)
        live = maintenance.status(host, entry, release.release_path(host, entry, version)) if version else None
        return {"instance": entry, "state": state, "maintenance": live,
                "live_version": version, "services": release.services(host, entry),
                **({"enablement_migration": enablement_migration} if enablement_migration else {})}
    if action in ("down", "up"):
        require(request.get("confirm") is True, "confirmation_required", "Confirm the maintenance change.")
        extra = maintenance.arguments(action, {key: request[key] for key in maintenance.FIELDS if key in request})
        with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
            _, entry = host.require_identity(project, profile)
            version = release.current_version(host, entry)
            require(version is not None, "no_release", "No release has been deployed.")
            path = release.release_path(host, entry, version)
            release.verified_release(host, entry, read_json(path / "manifest.json"))
            result = maintenance.run(host, entry, path, action, extra)
            if action == "up":
                require(release.ready(host, entry), "readiness_failed",
                        "Maintenance mode is off but the release did not become ready; inspect ACTION=logs ROLE=http.")
            return {"maintenance": {"down": result["down"]},
                    **({"bypass_path": result["bypass_path"]} if "bypass_path" in result else {})}
    if action in ("restart", "stop"):
        with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
            registry, entry = host.require_identity(project, profile)
            role = request.get("role", "all")
            # Naming a topology's only role selects the whole instance.
            if set(selected_units(entry, role)) == set(instance_units(entry)):
                role = "all"
            state = host.load_state(entry)
            release.validate_runtime_environment(host, entry)
            require(role == "all" or not (state.get("last_attempt") or {}).get("role_recovery_pending"),
                    "role_recovery", "Complete pending background recovery with a plain restart before selecting a role.")
            if action == "restart":
                release.resume_terminal_obligations(host, registry, entry, state)
            else:
                release.require_idle(state)
                release.cleanup_configuration_candidate(host, entry, state)
                release.resume_redis_transition(host, registry, entry, state)
                if (state.get("last_attempt") or {}).get("rotation_operation_id"):
                    release.save_rotation_outcome(host, entry, state["last_attempt"])
            release.check_live_state(host, entry, state)
            require(state["current"] is not None, "no_release", "No release has been deployed.")
            release.verified_release(host, entry, state["current"])
            release.begin(host, entry, state, action, state["current"]["version"], metadata={"role": role})
            release.phase(host, entry, state, "health")
            try:
                if action == "stop":
                    release.stop(host, entry, role=role, disable=True)
                    state["status"] = "stopped" if role == "all" else "degraded"
                else:
                    release.restart(host, entry, state, role=role)
                    state["status"] = "healthy" if role == "all" else "degraded"
                release.finish(host, entry, state, "succeeded")
            except (DeployError, OSError) as error:
                state["status"] = "degraded" if role != "all" or action == "stop" else "failed"
                release.finish(host, entry, state, "failed", error.code if isinstance(error, DeployError) else "filesystem_failed")
                raise
            return {"ok": True, "instance": entry["name"]}
    raise DeployError("unknown_action", "Unknown deployment action.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=ACTIONS)
    args = parser.parse_args()
    try:
        request = parse_json(sys.stdin.buffer.read(MAX_JSON + 1))
        result = execute(Host(), args.action, request)
        print(json.dumps(result, separators=(",", ":")))
        return 0
    except DeployError as error:
        print(json.dumps({"error": {"code": error.code, "detail": error.detail}}))
    except (OSError, ValueError, KeyError, TypeError):
        # Never expose malformed config values, subprocess inputs or credentials.
        print(json.dumps({"error": {"code": "host_operation_failed", "detail": "Host operation could not complete; inspect its registry and operation state."}}))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
