"""Private, bounded helpers for disposable deployment acceptance only."""
import json
import os
from pathlib import Path
import signal
import subprocess
import time
import uuid

WORK = Path("/var/lib/foundry-acceptance-f4a")
LAUNCHER = "/usr/local/sbin/foundry-host"
ROOTS = "/etc/ssl/certs/ca-certificates.crt"


def guard():
    if (os.geteuid() != 0 or os.environ.get("FOUNDRY_DISPOSABLE_HOST") != "1"
            or not Path("/.dockerenv").is_file()):
        raise RuntimeError("Requires an explicitly disposable root Docker host")
    if __debug__ is False:
        raise RuntimeError("Assertions must be enabled")
    WORK.mkdir(mode=0o700, exist_ok=True)
    os.umask(0o077)


def run(argv, *, input=None, env=None, timeout=120, check=True):
    # Never attach captured command output, argv, or stdin to exceptions: they
    # may contain app config, PostgreSQL diagnostics, or recovery key material.
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, start_new_session=True,
                               env={**os.environ, **(env or {})})
    try:
        stdout, stderr = process.communicate(input, timeout=timeout)
    except BaseException:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        raise RuntimeError("Acceptance subprocess interrupted (output withheld)") from None
    result = subprocess.CompletedProcess(argv, process.returncode, stdout, stderr)
    if check and result.returncode:
        raise RuntimeError("Acceptance subprocess failed (output withheld)")
    return result


def private_json(path, value):
    with path.open("x", encoding="utf-8") as stream:
        json.dump(value, stream)
    path.chmod(0o600)


def wait_for(probe, message, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = probe()
        if value:
            return value
        time.sleep(0.2)
    raise AssertionError(message)


def service(unit):
    values = run(["systemctl", "show", unit, "-p", "MainPID", "-p", "ActiveState"]).stdout
    value = dict(line.split("=", 1) for line in values.splitlines() if "=" in line)
    assert value["ActiveState"] == "active" and int(value["MainPID"]) > 0, "Service is not active"
    pid = value["MainPID"]
    ticks = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[19]
    return {"pid": int(pid), "start_ticks": ticks}


def fixture():
    # Schema-2 helpers expose acceptance_host.fixtures(), Host.require_identity(),
    # paths.instance_config() and load_state(); registry entries use DESIGN §4.
    from acceptance_host import Host, fixtures
    host = Host()
    project, profile = fixtures()[0]
    _, entry = host.require_identity(project, profile)
    assert host.read_registry()["schema"] == 2
    assert host.load_state(entry)["current"] is not None
    return host, project, profile, entry


def request(project, profile, **fields):
    # The launcher accepts one action and a JSON stdin request, retaining
    # the inherited project/profile envelope plus a persisted operation UUID.
    from upgrades import protocol
    metadata = protocol()
    return {"protocol": metadata["protocol"], "client": {"generation": metadata["generation"]},
            "project": project, "profile": profile, "confirm": True,
            "operation_id": str(uuid.uuid4()), **fields}


def action(name, payload, timeout=600):
    result = json.loads(run([LAUNCHER, name], input=json.dumps(payload), timeout=timeout).stdout)
    assert result.get("ok") is True, "Host action did not succeed"
    return result


def postgres_settings(host, entry):
    shared = host.read_registry()["shared"]["postgresql"]
    allocation = entry["allocations"]
    secrets = json.loads((host.paths.instance_config(entry["name"]) / "secrets.json").read_text())
    # Schema-2 helpers expose the private database_password secret key.
    return {"host": "localhost", "port": shared["port"], "user": allocation["database_role"],
            "database": allocation["database"], "password": secrets["database_password"],
            "sslmode": "verify-full", "sslrootcert": ROOTS}


def psql(settings, sql, *, database=None, check=True):
    return run(["psql", "-X", "-qAt", "--no-password", "-v", "ON_ERROR_STOP=1",
                "-h", settings["host"], "-p", str(settings["port"]), "-U", settings["user"],
                "-d", database or settings["database"]], input=sql,
               env={"PGPASSWORD": settings["password"], "PGSSLMODE": settings["sslmode"],
                    "PGSSLROOTCERT": settings["sslrootcert"], "PGCONNECT_TIMEOUT": "5",
                    "PGOPTIONS": "-c statement_timeout=15000"}, check=check)


def postgres_denial(settings, other_database):
    """T1: same endpoint, role, password and TLS; only the database changes."""
    assert other_database != settings["database"]
    assert settings["sslmode"] == "verify-full"
    assert psql(settings, "SELECT 1;").stdout.strip() == "1", "T1 own database failed"
    denied = psql(settings, "SELECT 1;", database=other_database, check=False)
    assert denied.returncode != 0, "Cross-database connection succeeded"
    # A dead port, missing database, or bad certificate is not authorization.
    assert any(text in denied.stderr.lower() for text in (
        "pg_hba.conf rejects connection", "no pg_hba.conf entry", "permission denied for database"
    )), "Denial was not a database authorization refusal"
    assert psql(settings, "SELECT 1;").stdout.strip() == "1", "T1 post-denial control failed"
