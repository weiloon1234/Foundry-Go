#!/usr/bin/env python3
"""SSH host entrypoint. JSON request on stdin; one safe JSON result on stdout."""
import argparse
import json
import os
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, MAX_JSON, check_path, identity, parse_env, parse_json, require
from topology import instance_units
from host import Host

ACTIONS = ("hello", "sync", "tools-plan", "tools-apply", "upgrade-resume", "inspect", "bootstrap-plan", "bootstrap-apply", "activate", "status", "rollback", "restart", "stop",
           "logs", "config-keys", "config-set", "reconcile", "report")


def execute(host, action, request):
    from provision import inspect_host, bootstrap_plan, bootstrap_apply
    from report import collect_report
    import release
    import upgrades

    require(isinstance(request, dict), "invalid_request", "Request must be a JSON object.")
    if action == "hello":
        return upgrades.hello(host)
    common = {"protocol", "client", "project", "profile", "operation_id", "confirm"}
    arguments = {"bootstrap-plan": {"topology"}, "bootstrap-apply": {"topology", "plan_token"},
                 "activate": {"archive", "archive_sha256"}, "config-set": {"values"},
                 "tools-apply": {"plan_token"}, "upgrade-resume": {"plan_token"}}
    if action not in upgrades.READ_ACTIONS:
        require(set(request) <= common | arguments.get(action, set()), "invalid_request", "Unknown mutation request field.")
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
    if action == "bootstrap-plan":
        return bootstrap_plan(host, project, profile, topology=request.get("topology"))
    if action == "bootstrap-apply":
        require(request.get("confirm") is True, "confirmation_required", "Review and confirm the bootstrap plan before applying it.")
        # The apply path replans under its host lock. Its token detects drift
        # between the operator's approved plan and that fresh snapshot.
        return bootstrap_apply(host, project, profile, Path(__file__).resolve().parents[1],
                               plan_token=request.get("plan_token"), topology=request.get("topology"))
    if action == "sync":
        registry = host.read_registry()
        require(registry["host_id"] == profile["host"]["host_id"], "host_mismatch", "Host binding differs.")
        entry = registry["instances"].get(profile["instance_id"])
        require(entry is not None and entry["project_id"] == project["id"], "identity_mismatch", "Unknown project/instance UUID.")
        return {"profile": entry["profile"], "topology": entry["topology"]}
    _, entry = host.require_identity(project, profile)
    if action == "activate":
        require(isinstance(request.get("archive"), str), "invalid_request", "A staged release archive is required.")
        return release.activate(host, project, profile, request["archive"], request.get("archive_sha256"))
    if action == "rollback":
        return release.rollback(host, project, profile)
    if action == "reconcile":
        require(request.get("confirm") is True, "confirmation_required", "Inspect status and confirm reconciliation first.")
        return release.reconcile(host, project, profile)
    if action == "config-set":
        require(request.get("confirm") is True, "confirmation_required", "Confirm this explicit configuration change.")
        return release.config_set(host, project, profile, request.get("values"))
    if action == "config-keys":
        values = parse_env(check_path(host.paths.instance_config(entry["name"]) / "app.env", "file").read_text())
        return {"keys": sorted(values), "locked_keys": sorted(release.PIPELINE_KEYS), "configuration_revision": entry["configuration_revision"]}
    if action == "logs":
        # Explicit operator action. Logs are never included in the host report.
        units = instance_units(entry)
        role = request.get("role", "all")
        require(role == "all" or role in units, "invalid_role", "Select a declared topology role.")
        selected = units.values() if role == "all" else [units[role]]
        result = host.run(["journalctl", *[arg for unit in selected for arg in ("--unit", unit)], "--lines=100", "--no-pager", "--output=short-iso"], timeout=15)
        return {"instance": entry["name"], "logs": result.stdout}
    if action == "status":
        return {"instance": entry, "state": host.load_state(entry), "live_version": release.current_version(host, entry), "services": release.services(host, entry)}
    if action in ("restart", "stop"):
        with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
            _, entry = host.require_identity(project, profile)
            release.validate_runtime_environment(host, entry)
            state = host.load_state(entry)
            release.check_live_state(host, entry, state)
            require(state["current"] is not None, "no_release", "No release has been deployed.")
            release.verified_release(host, entry, state["current"])
            release.begin(host, entry, state, action, state["current"]["version"])
            release.phase(host, entry, state, "health")
            if action == "stop":
                release.stop(host, entry)
                state["status"] = "stopped"
                release.finish(host, entry, state, "succeeded")
                return {"ok": True, "instance": entry["name"]}
            release.restart(host, entry)
            state["status"] = "healthy" if release.ready(host, entry) else "failed"
            release.finish(host, entry, state, "succeeded" if state["status"] == "healthy" else "failed")
            require(state["status"] == "healthy", "readiness_failed", "Service did not become ready after restart.")
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
    except NotImplementedError:
        print(json.dumps({"error": {"code": "lane_not_implemented", "detail": "This foundation bundle requires the F1/F2/F3 implementations before host deployment."}}))
    except (OSError, ValueError, KeyError, TypeError):
        # Never expose malformed config values, subprocess inputs or credentials.
        print(json.dumps({"error": {"code": "host_operation_failed", "detail": "Host operation could not complete; inspect its registry and operation state."}}))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
