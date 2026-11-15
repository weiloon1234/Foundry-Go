"""Production deployment phases inside an explicitly disposable rehearsal host."""
import json
import os
from pathlib import Path
import sys
import time
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import unit_selection_names, unit_selection_identities
from contracts import DeployError, check_path, digest, identity, read_json, require
from host import Host
from containers import run
import containers
import uploads
import release_review

WORK = Path("/var/lib/foundry-rehearsal")
TOOLS = Path(__file__).resolve().parents[1]


def guard():
    require(os.geteuid() == 0 and Path("/.dockerenv").is_file()
            and os.environ.get("FOUNDRY_DISPOSABLE_HOST") == "1"
            and os.environ.get("FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT") == "1",
            "rehearsal_host", "Requires an explicitly disposable root Docker host.")
    marker = read_json(check_path(WORK / "context.json", "file"))
    require(marker["run_id"] == os.environ.get("FOUNDRY_REHEARSAL_RUN_ID"),
            "rehearsal_host", "Disposable run identity differs.")
    from rehearsal import acceptance_profile
    acceptance_profile(marker["project"], marker["profile"], marker["topology"])
    return marker


def request(context, **extra):
    from upgrades import protocol_metadata
    return {**protocol_metadata(TOOLS), "project": context["project"], "profile": context["profile"],
            "operation_id": str(uuid.uuid4()), "confirm": True, **extra}


def action(name, context, *, bootstrap=False, **extra):
    command = [sys.executable, "-B", str(TOOLS / "server/manage.py")] if bootstrap else ["/usr/local/sbin/foundry-host"]
    result = run([*command, name], input=json.dumps(request(context, **extra)), timeout=1800, check=False)
    try:
        value = json.loads(result.stdout)
    except ValueError:
        raise DeployError("rehearsal_action", "Production action returned no valid response.") from None
    if result.returncode or "error" in value or (value.get("ok") is False and name != "release-plan"):
        code = value.get("error", {}).get("code", "rehearsal_action")
        raise DeployError(code, "Production rehearsal action refused; private output withheld.")
    return value


class MissingServiceSecrets(DeployError):
    def __init__(self, names):
        self.names = sorted(names)
        super().__init__("rehearsal_service_secrets_missing", "Supply rehearsal binding configuration: " + ", ".join(self.names))


def error_result(error):
    result = {"ok": False, "error": error.code if isinstance(error, DeployError) else "rehearsal_phase_failed"}
    if isinstance(error, MissingServiceSecrets):
        result["missing_service_secrets"] = error.names
    return result


def release_secret_configuration(requirements):
    supplied = read_json(WORK / "secrets.json").get("configuration", {})
    missing = [row["key"] for row in requirements if not row["present"] and not supplied.get(row["key"])]
    if missing:
        raise MissingServiceSecrets(missing)
    # Bindings also contain application settings, which release review does not
    # accept. Only the plan's missing service/copy keys belong in this request.
    return {row["key"]: supplied[row["key"]] for row in requirements if not row["present"]}


def activate(context):
    evidence = {"operation_id": context["activation_operation_id"]}
    try:
        archive = WORK / "app.tar.gz"
        require(digest(archive) == context["archive_sha256"], "rehearsal_archive", "Copied archive digest differs.")
        activation = {"archive": str(archive), "archive_sha256": context["archive_sha256"], **evidence}
        status = action("status", context)
        if release_review.required(context["profile"], status):
            plan = release_review.review(lambda name, arguments: action(name, context, **arguments),
                                         activation, release_secret_configuration)
            evidence["release_plan"] = plan
            require(plan["ok"], "rehearsal_release_review", "Rehearsal release review is incomplete.")
        return {**action("activate", context, **activation), **evidence}
    except Exception as error:
        return {**evidence, **error_result(error)}


def prepare(context):
    host = Host()
    require(host.read_registry(missing=True) is None, "rehearsal_host", "Rehearsal requires a fresh unregistered host.")
    host.run(["ufw", "default", "deny", "incoming"])
    for port in (22, 80, 443):
        host.run(["ufw", "allow", str(port) + "/tcp"])
    host.run(["ufw", "logging", "low"])
    host.run(["ufw", "--force", "enable"])
    # Only package-maintenance setup has external access; no application runs yet.
    host.run(["apt-get", "update", "-qq"], timeout=300)
    host.run(["apt-get", "-y", "upgrade"], env={"DEBIAN_FRONTEND": "noninteractive"}, timeout=1200)
    settings = read_json(WORK / "secrets.json")
    store = settings["backup_s3"]
    environment = {"RCLONE_CONFIG_REHEARSAL_TYPE": "s3", "RCLONE_CONFIG_REHEARSAL_PROVIDER": "Other",
        "RCLONE_CONFIG_REHEARSAL_ENDPOINT": store["endpoint"],
        "RCLONE_CONFIG_REHEARSAL_ACCESS_KEY_ID": store["access_key_id"],
        "RCLONE_CONFIG_REHEARSAL_SECRET_ACCESS_KEY": store["secret_access_key"],
        "RCLONE_CONFIG_REHEARSAL_REGION": store["region"], "RCLONE_CONFIG_REHEARSAL_ENV_AUTH": "false"}
    deadline = time.monotonic() + 120
    while True:
        result = host.run(["rclone", "lsd", "rehearsal:", "--retries=1", "--low-level-retries=1",
                           "--contimeout=3s", "--timeout=3s"], env=environment, timeout=10, check=False)
        if result.returncode == 0:
            break
        require(time.monotonic() < deadline, "rehearsal_storage", "Disposable object store did not become ready.")
        time.sleep(1)
    buckets = {store["bucket"], *context["storage_buckets"]}
    for bucket in sorted(buckets):
        host.run(["rclone", "mkdir", "rehearsal:" + bucket, "--retries=1", "--low-level-retries=1"],
                 env=environment, timeout=30)
    identity = WORK / "age-identity.txt"
    host.run(["age-keygen", "-o", str(identity)])
    settings["age_recipients"] = [host.run(["age-keygen", "-y", str(identity)]).stdout.strip()]
    (WORK / "secrets.json").write_text(json.dumps(settings))
    return {"ok": True}


def observe(context, *, stopped=False):
    from report import collect_report
    from topology import instance_units
    host = Host()
    _, entry = host.require_identity(context["project"], context["profile"])
    units = instance_units(entry)
    if stopped:
        for unit in units.values():
            result = host.run(["systemctl", "show", unit, "--property=ActiveState,MainPID,ControlPID", "--no-pager"])
            values = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
            require(values.get("ActiveState") in ("inactive", "failed") and values.get("MainPID") == "0"
                    and values.get("ControlPID") == "0", "rehearsal_stop", "A selected service still runs after stop.")
    elif context["topology"]["schema"] == 2:
        from feature_lifecycle import load
        from services import wait_ready_set
        _, selection, inventory, _ = load(host, entry)
        require(wait_ready_set(host, inventory, unit_selection_identities(inventory.profile.instance_id, selection.enabled_roles), time.monotonic() + 120),
                "readiness_failed", "Selected services did not pass production readiness.")
    else:
        from release import ready
        require(ready(host, entry, timeout=120), "readiness_failed", "Application did not pass production readiness.")
    from rehearsal_storage import collect
    storage_fidelity = collect(host, units)
    from listeners import observe_sockets
    sockets = observe_sockets(host)
    return {"ok": True, "ready": not stopped, "units": dict(zip(unit_selection_names(units), units.values())), "report": {**collect_report(host), "storage_fidelity": storage_fidelity}, "storage_fidelity": storage_fidelity,
            "udp_sockets": [{"address": row.socket.address, "port": row.socket.port}
                            for row in sockets if row.socket.transport == "udp"],
            "public_udp": "unavailable"}


def phase(name, context):
    if name == "prepare":
        return prepare(context)
    if name in ("upload-volumes", "release-upload-volumes"):
        from topology import instance_units, validate_topology
        entry = {**identity(context["project"], context["profile"]),
                 "profile": context["profile"], "topology": validate_topology(context["topology"])}
        workdir = WORK / "upload-volumes"
        if name == "release-upload-volumes":
            if workdir.exists():
                # Failed activation can leave services holding files open.
                run(["systemctl", "stop", "nginx.service", *instance_units(entry).values()], check=False)
            containers.release_upload_volumes(workdir)
            return {"ok": True}
        value = context["profile"]["uploads"]
        paths = uploads.volume_paths(entry)
        containers.provision_upload_volumes(paths, value["pools"], workdir)
        return {"ok": True, "upload_storage": {"status": "reduced", "backing": "loop-mounted sparse ext4",
                "pools": {name: {"mount": str(path), "image_bytes": value["pools"][name]}
                          for name, path in paths.items()},
                "real_block_device_certified": False, "quota_enforcement_certified": False}}
    if name == "bootstrap-plan":
        result = action(name, context, bootstrap=True, topology=context["topology"])
        require(result.get("host_id") == context["profile"]["host"]["host_id"], "rehearsal_identity", "Bootstrap changed host identity.")
        (WORK / "plan.json").write_text(json.dumps(result))
        return {"ok": True, "plan": result}
    if name == "bootstrap-apply":
        plan = read_json(WORK / "plan.json")
        release_secret_configuration(plan.get("observations", {}).get("required_service_secrets", []))
        return action(name, context, bootstrap=True, topology=context["topology"],
                      plan_token=plan["plan_token"], secrets=read_json(WORK / "secrets.json"))
    if name == "activate":
        return activate(context)
    if name in ("restart", "stop"):
        return action(name, context)
    if name in ("observe", "stopped"):
        return observe(context, stopped=name == "stopped")
    raise DeployError("rehearsal_phase", "Unknown rehearsal phase.")


if __name__ == "__main__":
    os.umask(0o077)
    try:
        result = phase(sys.argv[1], guard())
    except Exception as error:
        # Even application exception formatting can disclose private values.
        result = error_result(error)
    print(json.dumps(result))
    sys.exit(0 if result.get("ok", True) else 1)
