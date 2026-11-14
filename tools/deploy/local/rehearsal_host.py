"""Production deployment phases inside an explicitly disposable rehearsal host."""
import json
import os
from pathlib import Path
import sys
import time
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, check_path, digest, read_json, require
from host import Host
from containers import run

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
    if result.returncode or "error" in value or value.get("ok") is False:
        code = value.get("error", {}).get("code", "rehearsal_action")
        raise DeployError(code, "Production rehearsal action refused; private output withheld.")
    return value


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
        require(wait_ready_set(host, inventory, selection.enabled_roles, time.monotonic() + 120),
                "readiness_failed", "Selected services did not pass production readiness.")
    else:
        from release import ready
        require(ready(host, entry, timeout=120), "readiness_failed", "Application did not pass production readiness.")
    from listeners import observe_sockets
    sockets = observe_sockets(host)
    return {"ok": True, "ready": not stopped, "units": units, "report": collect_report(host),
            "udp_sockets": [{"address": row.socket.address, "port": row.socket.port}
                            for row in sockets if row.socket.transport == "udp"],
            "public_udp": "unavailable"}


def phase(name, context):
    if name == "prepare":
        return prepare(context)
    if name == "bootstrap-plan":
        result = action(name, context, bootstrap=True, topology=context["topology"])
        require(result.get("host_id") == context["profile"]["host"]["host_id"], "rehearsal_identity", "Bootstrap changed host identity.")
        (WORK / "plan.json").write_text(json.dumps(result))
        return {"ok": True, "plan": result}
    if name == "bootstrap-apply":
        return action(name, context, bootstrap=True, topology=context["topology"],
                      plan_token=read_json(WORK / "plan.json")["plan_token"], secrets=read_json(WORK / "secrets.json"))
    if name == "activate":
        archive = WORK / "app.tar.gz"
        require(digest(archive) == context["archive_sha256"], "rehearsal_archive", "Copied archive digest differs.")
        return action(name, context, archive=str(archive), archive_sha256=context["archive_sha256"])
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
        result = {"ok": False, "error": error.code if isinstance(error, DeployError) else "rehearsal_phase_failed"}
    print(json.dumps(result))
    sys.exit(0 if result.get("ok", True) else 1)
