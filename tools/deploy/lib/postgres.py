"""Dedicated, journaled PostgreSQL and shared host-local certificate ownership."""
from __future__ import annotations

import json
import os
from pathlib import Path
import pwd
import re
import secrets
import stat
import uuid

from host import Host
from contracts import (DeployError, HOST_PATHS, POSTGRES_PORT, check_path, instance_paths,
                       marker, read_json, require)
import packages

ROOTS = "/etc/ssl/certs/ca-certificates.crt"
SOCKET = "/run/foundry/postgresql"
CLUSTER_HOME = "/var/lib/foundry/postgresql"
TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"
CAPACITY = {"budget_mib": 1024, "max_connections": 40, "shared_buffers": "128MB",
            "work_mem": "4MB", "maintenance_work_mem": "64MB"}


def cluster_owner() -> str:
    """The OS and peer database identity of the dedicated Foundry cluster."""
    return "foundry-postgres"

def _directory(host, path, mode=0o700, owner="root", group="root"):
    check_path(path, "directory")
    path.mkdir(parents=True, exist_ok=True, mode=mode)
    if host.paths.root == Path("/"):
        host.run(["chown", owner + ":" + group, str(path)])
    os.chmod(path, mode)


def _private(host, name):
    directory = host.paths.p(HOST_PATHS["private"])
    host.check_control_directory(directory)
    return check_path(directory / name, "file")


def _read_private(host, path):
    if not path.exists():
        return None
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "postgres_ownership", "Shared-service journals must be root-owned and private.")
    return read_json(path)


def _write_private(host, path, value):
    _directory(host, path.parent)
    host.owned_write(path, json.dumps(value, sort_keys=True) + "\n", mode=0o600)


def _openssl_publish(host, command, path, mode):
    temporary = check_path(path.with_name("." + path.name + ".pending"), "file")
    host.run(command + ["-out", str(temporary)], timeout=60)
    host.owned_write(path, temporary.read_bytes(), mode=mode)
    temporary.unlink()


def prepare_tls(host: Host, registry: dict) -> dict:
    """Install the host CA source; only authenticated service probes verify trust."""
    missing = [name for name, binary in (("openssl", "/usr/bin/openssl"), ("ca-certificates", "/usr/sbin/update-ca-certificates"))
               if not host.paths.p(binary).is_file()]
    if missing:
        packages.install_missing(host, missing)
    directory = host.paths.p(HOST_PATHS["pki"])
    journal_path = _private(host, "tls.json")
    journal = _read_private(host, journal_path)
    if journal is None:
        require(not directory.exists() or not any(directory.iterdir()), "tls_ownership",
                "An unclaimed Foundry PKI directory already exists.")
        journal = {"host_id": registry["host_id"], "phase": "creating", "source_installed": False,
                   "trust_verified": False, "trust_probes": [], "refresh_pending": False}
        _write_private(host, journal_path, journal)
    require(journal.get("host_id") == registry["host_id"], "tls_ownership", "Foundry CA host identity mismatch.")
    _directory(host, directory)
    ca, ca_key = directory / "ca.crt", directory / "ca.key"
    cert, key = directory / "localhost.crt", directory / "localhost.key"
    for path in (ca, ca_key, cert, key):
        check_path(path, "file")
    # Private staging permits resuming a key-only interruption without changing
    # any authority already published to services or the system trust store.
    if not ca_key.exists():
        require(not ca.exists() and not journal["source_installed"], "tls_partial", "Restore the missing Foundry CA key.")
        _openssl_publish(host, ["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072"], ca_key, 0o600)
    if not ca.exists():
        require(not journal["source_installed"], "tls_partial", "Restore the missing published Foundry CA.")
        _openssl_publish(host, ["openssl", "req", "-x509", "-new", "-key", str(ca_key), "-sha256", "-days", "3650",
                  "-subj", "/CN=Foundry " + registry["host_id"],
                  "-addext", "basicConstraints=critical,CA:TRUE,pathlen:0",
                  "-addext", "keyUsage=critical,keyCertSign,cRLSign"], ca, 0o644)
    if not key.exists():
        require(not cert.exists() and not journal["source_installed"], "tls_partial", "Restore the missing Foundry server key.")
        _openssl_publish(host, ["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072"], key, 0o600)
    if not cert.exists():
        require(not journal["source_installed"], "tls_partial", "Restore the missing published Foundry server certificate.")
        csr, ext = directory / "localhost.csr", directory / "localhost.ext"
        host.owned_write(ext, "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1\nextendedKeyUsage=serverAuth\n", mode=0o600)
        host.run(["openssl", "req", "-new", "-key", str(key), "-out", str(csr), "-subj", "/CN=localhost"], timeout=60)
        _openssl_publish(host, ["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca), "-CAkey", str(ca_key),
                  "-set_serial", "0x" + secrets.token_hex(16), "-days", "3650", "-sha256", "-extfile", str(ext)], cert, 0o644)
        csr.unlink(missing_ok=True)
        ext.unlink(missing_ok=True)
    for path in (ca_key, key):
        require(not path.stat().st_mode & 0o077, "tls_ownership", "Foundry signing keys must remain private.")
    for certificate, private_key in ((ca, ca_key), (cert, key)):
        public = host.run(["openssl", "x509", "-in", str(certificate), "-pubkey", "-noout"]).stdout
        expected_public = host.run(["openssl", "pkey", "-in", str(private_key), "-pubout"]).stdout
        require(public == expected_public, "tls_partial", "Foundry certificate and private key do not match.")
    host.run(["openssl", "verify", "-CAfile", str(ca), "-verify_hostname", "localhost", str(cert)])
    host.run(["openssl", "x509", "-checkend", "2592000", "-noout", "-in", str(cert)])
    trust = check_path(host.paths.p(f"/usr/local/share/ca-certificates/foundry-{registry['host_id']}.crt"), "file")
    require(not trust.exists() or trust.read_bytes() == ca.read_bytes(), "tls_ownership", "Foundry trust source conflicts with an existing authority.")
    host.owned_write(trust, ca.read_bytes())
    journal.update(phase="source_installed", source_installed=True, refresh_pending=True, trust_verified=False)
    _write_private(host, journal_path, journal)
    registry.setdefault("shared", {})["tls"] = {key: journal[key] for key in ("source_installed", "trust_verified", "trust_probes", "refresh_pending")}
    host.journal(registry)
    # An equal source file does not prove update-ca-certificates finished (A06).
    # Always refreshing also repairs a removed/rebuilt system bundle on retry.
    host.run(["update-ca-certificates"], timeout=120)
    host.run(["openssl", "verify", "-CAfile", str(host.paths.p(ROOTS)), "-verify_hostname", "localhost", str(cert)])
    journal["refresh_pending"] = False
    _write_private(host, journal_path, journal)
    registry["shared"]["tls"]["refresh_pending"] = False
    host.journal(registry)
    return {"ca": ca, "cert": cert, "key": key}


def record_trust_probe(host: Host, registry: dict, service: str) -> None:
    require(service in {"postgresql", "redis"}, "tls_probe", "Unknown Foundry TLS service.")
    path = _private(host, "tls.json")
    journal = _read_private(host, path)
    require(journal and journal.get("host_id") == registry["host_id"] and journal.get("source_installed") and not journal.get("refresh_pending"),
            "tls_ownership", "CA source must be installed before recording verified trust.")
    journal["trust_probes"] = sorted(set(journal["trust_probes"]) | {service})
    journal["trust_verified"] = journal["trust_probes"] == ["postgresql", "redis"]
    journal["phase"] = "trust_verified" if journal["trust_verified"] else "source_installed"
    _write_private(host, path, journal)
    registry["shared"]["tls"] = {key: journal[key] for key in ("source_installed", "trust_verified", "trust_probes", "refresh_pending")}
    host.journal(registry)


def _major(host):
    return packages.ubuntu_major(host)


def _listeners(host):
    result = host.run(["ss", "-Hltn"])
    ports = set()
    for line in result.stdout.splitlines():
        columns = line.split()
        match = re.search(r":([0-9]+)$", columns[3]) if len(columns) >= 4 else None
        require(match is not None, "postgres_uninspectable", "Cannot inspect listening ports safely.")
        ports.add(int(match[1]))
    return ports


def _inventory(host):
    if not host.paths.p("/usr/bin/pg_lsclusters").exists():
        if host.paths.p("/etc/postgresql").exists():
            require(packages.inspect(host).get("state") == "resume", "postgres_uninspectable",
                    "PostgreSQL metadata exists without its cluster inspection tool or an owned package transaction.")
        return []
    result = host.run(["pg_lsclusters", "--no-header"])
    rows = []
    for line in result.stdout.splitlines():
        cells = line.split()
        require(len(cells) >= 7 and cells[0].isdigit() and cells[2].isdigit(), "postgres_uninspectable", "Cannot inspect PostgreSQL cluster inventory.")
        rows.append({"major": int(cells[0]), "cluster": cells[1], "port": int(cells[2]),
                     "status": cells[3], "owner": cells[4], "data_path": cells[5]})
    return rows


def _allocation(major, port, token):
    return {"owner": "foundry", "cluster": "foundry", "major": major, "version": major,
            "port": port, "socket_directory": SOCKET, "unit": f"postgresql@{major}-foundry.service",
            "config_path": f"/etc/postgresql/{major}/foundry", "data_path": f"{CLUSTER_HOME}/{major}/foundry",
            "cluster_owner": cluster_owner(),
            "ownership_token": token, "capacity": dict(CAPACITY), "archive_command": ""}


def _allocation_valid(value):
    if (not isinstance(value, dict) or value.get("major") not in (16, 18) or type(value.get("port")) is not int
            or not POSTGRES_PORT <= value["port"] <= POSTGRES_PORT + 99 or not value.get("ownership_token")):
        return False
    expected = _allocation(value["major"], value["port"], value["ownership_token"])
    return all(value.get(key) == expected[key] for key in ("owner", "cluster", "unit", "socket_directory", "config_path", "data_path", "capacity", "cluster_owner"))


def _cluster_journal(host, registry):
    value = _read_private(host, _private(host, "postgres-cluster.json"))
    if value is not None:
        require(isinstance(value, dict) and value.get("host_id") == registry["host_id"] and _allocation_valid(value.get("allocation", {})),
                "postgres_ownership", "PostgreSQL preparation journal identity mismatch.")
        require(value.get("phase") in {"reserved", "creating", "recreating", "configuring", "starting", "ready"},
                "postgres_ownership", "Unknown PostgreSQL preparation phase.")
    return value


def _save_cluster(host, value):
    _write_private(host, _private(host, "postgres-cluster.json"), value)


def _owner_path(host, allocation):
    return check_path(host.paths.p(allocation["config_path"]) / "foundry-owner.json", "file")


def _initial_creation(journal):
    return (journal is not None and journal["phase"] in {"creating", "recreating"}
            and journal.get("creation_intent") is True and journal.get("start_attempted") is False
            and journal.get("start_succeeded") is False and journal["allocation"].get("state") != "ready")


def inspect(host: Host, registry: dict) -> dict:
    """Read metadata first; only an explicitly owned cluster permits peer access."""
    major = _major(host)
    journal = _cluster_journal(host, registry)
    owned = registry.get("shared", {}).get("postgresql", {})
    if owned:
        require(_allocation_valid(owned) and owned["major"] == major, "postgres_ownership",
                "Foundry cluster allocation changed; an OS/major upgrade requires a migration plan.")
        require(journal and all(journal["allocation"].get(key) == owned.get(key)
                               for key in ("ownership_token", "major", "port", "config_path", "data_path", "socket_directory", "capacity")),
                "postgres_ownership", "Foundry cluster has no matching private creation journal.")
    elif journal:
        owned = journal["allocation"]
    # pg_lsclusters itself can fail on a half-written postgresql.conf. Only the
    # private never-started creation intent permits filesystem-based recovery.
    clusters = [] if _initial_creation(journal) else _inventory(host)
    foundry = [item for item in clusters if item["cluster"] == "foundry"]
    configuration = host.paths.p(f"/etc/postgresql/{major}/foundry")
    data = host.paths.p(f"{CLUSTER_HOME}/{major}/foundry")
    check_path(configuration, "directory")
    check_path(data, "directory")
    unit = f"postgresql@{major}-foundry.service"
    unit_path = check_path(host.paths.p("/etc/systemd/system/" + unit), "file")
    override = host.paths.p("/etc/systemd/system/" + unit + ".d")
    require(not override.exists() and not override.is_symlink(), "postgres_ownership",
            "An unclaimed override for the Foundry cluster unit exists.")
    if unit_path.exists():
        require(owned and journal["phase"] in {"configuring", "starting", "ready"}
                and unit_path.read_text() == _unit(owned), "postgres_ownership",
                "The Foundry PostgreSQL unit differs from its journaled ownership.")
    if not owned:
        require(not foundry and not configuration.exists() and not data.exists(), "postgres_ownership",
                "An unclaimed Foundry cluster or directory already exists.")
        require(not check_path(host.paths.p(f"/var/log/postgresql/postgresql-{major}-foundry.log"), "file").exists(),
                "postgres_ownership", "An unclaimed Foundry PostgreSQL log exists.")
        require(not host.paths.p(SOCKET).exists(),
                "postgres_ownership", "An unclaimed Foundry PostgreSQL runtime path exists.")
        ports = _listeners(host) | {item["port"] for item in clusters}
        port = next((number for number in range(POSTGRES_PORT, POSTGRES_PORT + 100) if number not in ports), None)
        require(port is not None, "postgres_ports_exhausted", "No free port in the Foundry PostgreSQL range.")
        return {"state": "absent", "major": major, "port": port, "cluster": "foundry", "owner": "foundry"}
    require(len(foundry) <= 1, "postgres_ownership", "Unexpected additional Foundry-named clusters.")
    if _initial_creation(journal):
        require(all(path == configuration for path in host.paths.p("/etc/postgresql").glob("*/foundry")),
                "postgres_ownership", "Unexpected additional Foundry-named clusters.")
    if foundry:
        row = foundry[0]
        require(row["major"] == major and row["port"] == owned["port"] and row["data_path"] == owned["data_path"]
                and row["owner"] == cluster_owner(), "postgres_ownership", "Foundry cluster metadata does not match its reservation.")
    owner_path = _owner_path(host, owned)
    if owner_path.exists():
        require(read_json(owner_path) == {"host_id": registry["host_id"], "ownership_token": owned["ownership_token"]},
                "postgres_ownership", "Foundry cluster ownership marker changed.")
    elif journal["phase"] not in {"reserved", "creating", "recreating"}:
        raise DeployError("postgres_ownership", "The claimed PostgreSQL cluster ownership marker is missing.")
    if owned.get("state") == "ready":
        require(foundry and foundry[0]["status"] == "online" and owner_path.exists() and unit_path.exists(), "postgres_unavailable",
                "The ready Foundry cluster is unavailable; reconcile its recorded allocation.")
        _verify_settings(host, registry)
        configuration = host.paths.p(owned["config_path"]) / "conf.d/foundry.conf"
        allowed = [_configuration(owned)]
        transition = journal.get("archive_transition", {})
        if transition.get("phase") == "publishing" and transition.get("old") == owned.get("archive_command", ""):
            allowed.append(_configuration(dict(owned, archive_command=transition["new"])))
        require(check_path(configuration, "file").read_text() in allowed,
                "postgres_configuration_drift", "Foundry cluster settings changed outside a shared-service plan.")
        hba = check_path(host.paths.p(owned["config_path"]) / "pg_hba.conf", "file")
        publication = journal.get("hba", {})
        allowed_hba = [publication.get("new")]
        if publication.get("phase") == "publishing":
            allowed_hba.append(publication.get("old"))
        require(hba.read_text() in allowed_hba,
                "postgres_hba_conflict", "Foundry HBA changed outside its publication journal.")
        return dict(owned)
    return dict(owned, state="preparing", phase=journal["phase"])


def cluster_settings(host: Host, registry: dict) -> dict:
    """Return the single allocated cluster's public paths and durability settings."""
    cluster = _cluster(registry)
    return dict(cluster, capacity=dict(cluster["capacity"]), ssl="on", password_encryption="scram-sha-256",
                fsync="on", full_page_writes="on", synchronous_commit="on",
                archive_mode="on", wal_level="replica")


def _configuration(allocation):
    values = {"port": str(allocation["port"]), "listen_addresses": "'127.0.0.1,::1'",
              "unix_socket_directories": _sql(SOCKET),
              "external_pid_file": _sql(SOCKET + "/postmaster.pid"), "password_encryption": "'scram-sha-256'", "ssl": "on",
              "ssl_cert_file": _sql(allocation["config_path"] + "/foundry.crt"),
              "ssl_key_file": _sql(allocation["config_path"] + "/foundry.key"),
              "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "wal_level": "replica",
              "archive_mode": "on", "archive_command": _sql(allocation.get("archive_command", "").replace("\\", "\\\\"))}
    # Sorted: the registry persists sorted JSON, so in-memory order must not decide the bytes.
    for key, value in sorted(allocation["capacity"].items()):
        if key != "budget_mib":
            values[key] = str(value) if isinstance(value, int) else _sql(value)
    return "# Foundry-owned cluster settings\n" + "".join(f"{key} = {value}\n" for key, value in values.items())


def _hba(registry):
    lines = ["# Foundry-owned exact database/role pairs", f'local all "{cluster_owner()}" peer', "local all all reject"]
    for entry in sorted(registry.get("instances", {}).values(), key=lambda item: item["profile"]["instance_id"]):
        expected = "f_" + uuid.UUID(entry["profile"]["instance_id"]).hex
        allocation = entry["allocations"]
        require(allocation["database"] == expected and allocation.get("database_role", expected) == expected,
                "postgres_ownership", "Instance database and role must match the UUID allocation.")
        lines.extend(f"hostssl {expected} {expected} {address} scram-sha-256" for address in ("127.0.0.1/32", "::1/128"))
    lines.extend(["hostnossl all all 127.0.0.1/32 reject", "hostnossl all all ::1/128 reject",
                  "host all all 0.0.0.0/0 reject", "host all all ::/0 reject"])
    return "\n".join(lines) + "\n"


def _publish_hba(host, registry, *, running):
    cluster = registry["shared"]["postgresql"]
    journal = _cluster_journal(host, registry)
    path = check_path(host.paths.p(cluster["config_path"]) / "pg_hba.conf", "file")
    content = _hba(registry)
    current = path.read_text() if path.exists() else None
    previous = journal.get("hba", {})
    if previous:
        allowed = [previous.get("new")]
        if previous.get("phase") == "publishing":
            allowed.append(previous.get("old"))
        require(current in allowed, "postgres_hba_conflict", "Foundry HBA differs from its recorded publication.")
    elif running:
        raise DeployError("postgres_hba_conflict", "Running Foundry HBA has no publication journal.")
    if previous.get("phase") == "published" and content == current:
        if running:
            errors = _psql(host, registry, "SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;")
            require(_text(errors) == "0", "postgres_hba_invalid", "Foundry HBA syntax validation failed.")
        return
    journal["hba"] = {"old": current, "new": content, "phase": "publishing"}
    _save_cluster(host, journal)
    host.owned_write(path, content, mode=0o640, owner=cluster_owner(), group=cluster_owner())
    if running:
        errors = _psql(host, registry, "SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;")
        require(_text(errors) == "0", "postgres_hba_invalid", "Foundry HBA syntax validation failed; reload withheld.")
        host.run(["pg_ctlcluster", str(cluster["major"]), "foundry", "reload"])
    journal["hba"]["phase"] = "published"
    _save_cluster(host, journal)


def _verify_settings(host, registry):
    cluster = _cluster(registry)
    observed = json.loads(_psql(host, registry, "SELECT json_build_object("
        "'port', current_setting('port'), 'socket', current_setting('unix_socket_directories'),"
        "'ssl', current_setting('ssl'), 'checksums', current_setting('data_checksums'),"
        "'encoding', current_setting('server_encoding'), 'password', current_setting('password_encryption'),"
        "'fsync', current_setting('fsync'), 'full_page_writes', current_setting('full_page_writes'),"
        "'synchronous_commit', current_setting('synchronous_commit'), 'listen', current_setting('listen_addresses'),"
        "'data', current_setting('data_directory'), 'config', current_setting('config_file'),"
        "'hba', current_setting('hba_file'), 'cert', current_setting('ssl_cert_file'),"
        "'key', current_setting('ssl_key_file'), 'archive_mode', current_setting('archive_mode'),"
        "'archive_command', current_setting('archive_command'), 'wal_level', current_setting('wal_level'),"
        "'max_connections', current_setting('max_connections'), 'shared_buffers', current_setting('shared_buffers'),"
        "'work_mem', current_setting('work_mem'), 'maintenance_work_mem', current_setting('maintenance_work_mem')); ").stdout)
    expected = {"port": str(cluster["port"]), "socket": SOCKET, "ssl": "on", "checksums": "on", "encoding": "UTF8",
                "password": "scram-sha-256", "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on",
                "listen": "127.0.0.1,::1", "data": cluster["data_path"], "config": cluster["config_path"] + "/postgresql.conf",
                "hba": cluster["config_path"] + "/pg_hba.conf", "cert": cluster["config_path"] + "/foundry.crt",
                "key": cluster["config_path"] + "/foundry.key", "archive_mode": "on", "wal_level": "replica"}
    expected.update({key: str(value) for key, value in cluster["capacity"].items() if key != "budget_mib"})
    commands = [cluster.get("archive_command", "")]
    pending = _cluster_journal(host, registry).get("archive_transition", {})
    if pending.get("phase") == "publishing" and pending.get("old") == commands[0]:
        commands.append(pending["new"])
    require(observed.pop("archive_command", None) in commands, "postgres_configuration_drift",
            "Foundry PostgreSQL archive command differs from its shared-service plan.")
    require(observed == expected, "postgres_configuration_drift", "Foundry PostgreSQL effective configuration differs from its ownership policy.")


def _archive_plan(cluster, plan):
    require(isinstance(plan, dict) and plan.get("owner") == "foundry" and plan.get("cluster") == "foundry"
            and plan.get("ownership_token") == cluster["ownership_token"]
            and isinstance(plan.get("archive_command"), str) and 0 < len(plan["archive_command"]) <= 4096
            and not any(character in plan["archive_command"] for character in "\r\n\x00"),
            "postgres_shared_plan", "Archive changes require an explicit plan for the allocated Foundry cluster.")
    return plan["archive_command"]


def _unit(allocation):
    content = (TEMPLATES / "foundry-postgresql.service").read_text()
    for key, value in {"OWNER": cluster_owner(), "MAJOR": str(allocation["major"]),
                       "PID_FILE": SOCKET + "/postmaster.pid"}.items():
        content = content.replace("@" + key + "@", value)
    return content


def _prepare_account(host, registry, journal):
    name = cluster_owner()
    account = host.run(["getent", "passwd", name], check=False)
    group = host.run(["getent", "group", name], check=False)
    require(account.returncode in (0, 2) and group.returncode in (0, 2),
            "postgres_account", "Cannot inspect the Foundry PostgreSQL account.")
    if not journal.get("account_intent"):
        require(account.returncode == group.returncode == 2
                and not host.paths.p(CLUSTER_HOME).exists(),
                "postgres_account", "An unclaimed Foundry PostgreSQL account, group or home exists.")
        journal["account_intent"] = True
        _save_cluster(host, journal)
    if group.returncode == 2:
        require(account.returncode == 2, "postgres_account", "The owned PostgreSQL group disappeared.")
        host.run(["groupadd", "--system", name])
        group = host.run(["getent", "group", name])
    fields = group.stdout.strip().split(":")
    require(len(fields) == 4 and fields[0] == name and fields[2].isdigit()
            and int(fields[2]) > 0 and not fields[3],
            "postgres_account", "The Foundry PostgreSQL group has foreign ownership or members.")
    marker = "foundry-host=" + registry["host_id"]
    if account.returncode == 2:
        require(not journal.get("start_attempted") and journal["phase"] != "ready",
                "postgres_account", "Restore the missing established PostgreSQL account.")
        host.run(["useradd", "--system", "--gid", name, "--home-dir", CLUSTER_HOME,
                  "--no-create-home", "--shell", "/usr/sbin/nologin", "--comment", marker, name])
        account = host.run(["getent", "passwd", name])
    user = account.stdout.strip().split(":")
    require(len(user) == 7 and user[0] == name and user[2].isdigit() and int(user[2]) > 0
            and user[3] == fields[2] and user[4:] == [marker, CLUSTER_HOME, "/usr/sbin/nologin"],
            "postgres_account", "The Foundry PostgreSQL account has foreign ownership.")
    _directory(host, host.paths.p(CLUSTER_HOME), 0o700, name, name)


def _discard_initialization(host, registry, journal):
    """Drop only an owned, never-started initdb tree; uncertain state is retained."""
    allocation = journal["allocation"]
    message = "Cluster initialization has data or uncertain start history; operator reconciliation is required."
    require(_initial_creation(journal), "postgres_partial_install", message)
    config = check_path(host.paths.p(allocation["config_path"]), "directory")
    data = check_path(host.paths.p(allocation["data_path"]), "directory")
    require(all(path == config for path in host.paths.p("/etc/postgresql").glob("*/foundry")),
            "postgres_partial_install", message)
    if host.paths.root == Path("/"):
        owner = pwd.getpwnam(cluster_owner()).pw_uid
        require((not data.exists() or data.stat().st_uid == owner)
                and (not config.exists() or config.stat().st_uid in (0, owner)),
                "postgres_partial_install", message)
    active = host.run(["systemctl", "is-active", "--quiet", allocation["unit"]], check=False)
    require(active.returncode in (3, 4) and allocation["port"] not in _listeners(host),
            "postgres_partial_install", message)
    # A killed provisioner may leave its process group running. Do not race a
    # surviving initdb or pg_createcluster, even though no server has started.
    for command in (["pgrep", "-u", cluster_owner()],
                    ["pgrep", "-f", r"(^|/| )pg_createcluster( |$)"]):
        require(host.run(command, check=False).returncode == 1, "postgres_partial_install", message)
    # initdb creates only the three built-in databases and low-OID relations.
    # postmaster.opts survives a clean shutdown; any start artifact forbids reset.
    directories = {"base", "base/1", "base/4", "base/5", "global", "pg_wal",
                   "pg_wal/archive_status", "pg_wal/summaries", "pg_xact", "pg_multixact",
                   "pg_multixact/members", "pg_multixact/offsets", "pg_commit_ts", "pg_dynshmem",
                   "pg_logical", "pg_logical/mappings", "pg_logical/snapshots", "pg_notify",
                   "pg_replslot", "pg_serial", "pg_snapshots", "pg_stat", "pg_stat_tmp",
                   "pg_subtrans", "pg_tblspc", "pg_twophase"}
    files = {"PG_VERSION", "postgresql.conf", "postgresql.auto.conf", "pg_hba.conf", "pg_ident.conf",
             "global/pg_control", "global/pg_filenode.map", "pg_logical/replorigin_checkpoint",
             "pg_wal/000000010000000000000001", "pg_xact/0000", "pg_subtrans/0000",
             "pg_multixact/members/0000", "pg_multixact/offsets/0000"}
    for path in data.rglob("*"):
        relative = path.relative_to(data).as_posix()
        info = path.lstat()
        relation = re.fullmatch(r"(?:base/(?:1|4|5)|global)/([0-9]+)(?:_(?:fsm|vm))?(?:\.[0-9]+)?", relative)
        catalog = bool(relation and 0 < int(relation[1]) < 16384)
        metadata = bool(re.fullmatch(r"base/(?:1|4|5)/(?:PG_VERSION|pg_filenode.map)", relative))
        require((stat.S_ISDIR(info.st_mode) and relative in directories)
                or (stat.S_ISREG(info.st_mode) and (relative in files or catalog or metadata)),
                "postgres_partial_install", message)
    control = data / "global/pg_control"
    if control.exists():
        result = host.run([f"/usr/lib/postgresql/{allocation['major']}/bin/pg_controldata", str(data)], check=False)
        require(result.returncode == 0 and re.search(r"(?m)^Database cluster state:\s+shut down\s*$", result.stdout),
                "postgres_partial_install", message)
    log = f"/var/log/postgresql/postgresql-{allocation['major']}-foundry.log"
    log_path = check_path(host.paths.p(log), "file")
    require(not log_path.exists() or log_path.stat().st_size == 0, "postgres_partial_install", message)
    for path in config.rglob("*"):
        relative = path.relative_to(config).as_posix()
        require((path.is_symlink() and relative == "log" and os.readlink(path) == log)
                or (not path.is_symlink() and path.is_dir() and relative == "conf.d")
                or (not path.is_symlink() and path.is_file() and relative in {
                    "postgresql.conf", "pg_hba.conf", "pg_ident.conf", "start.conf", "pg_ctl.conf",
                    "environment", "foundry-owner.json"}), "postgres_partial_install", message)
    journal["phase"] = "recreating"
    _save_cluster(host, journal)
    # pg_dropcluster needs configuration even when initdb died before publishing
    # it. Pin its deletion target instead of trusting a partial config's paths.
    _directory(host, config, owner=cluster_owner(), group=cluster_owner())
    host.owned_write(config / "postgresql.conf", "data_directory = " + _sql(allocation["data_path"])
                     + "\nport = " + str(allocation["port"]) + "\n", owner=cluster_owner(), group=cluster_owner())
    host.run(["pg_dropcluster", str(allocation["major"]), "foundry"], timeout=180)
    require(not config.exists() and not data.exists(), "postgres_partial_install", message)


def prepare(host: Host, registry: dict, *, shared_service_plan: dict | None = None) -> None:
    observed = inspect(host, registry)
    if observed["state"] == "ready":
        if shared_service_plan is None:
            prepare_tls(host, registry)
        pending = _cluster_journal(host, registry).get("archive_transition", {})
        if pending.get("phase") == "publishing":
            require(shared_service_plan is None or shared_service_plan.get("archive_command") == pending["new"],
                    "postgres_shared_plan", "Finish the pending archive publication before planning another change.")
            shared_service_plan = dict(observed, archive_command=pending["new"])
        if shared_service_plan is not None:
            cluster = registry["shared"]["postgresql"]
            command = _archive_plan(cluster, shared_service_plan)
            if command != cluster.get("archive_command", ""):
                journal = _cluster_journal(host, registry)
                journal["archive_transition"] = {"old": cluster.get("archive_command", ""), "new": command, "phase": "publishing"}
                _save_cluster(host, journal)
                changed = dict(cluster, archive_command=command)
                host.owned_write(host.paths.p(cluster["config_path"]) / "conf.d/foundry.conf", _configuration(changed),
                                 owner=cluster_owner(), group=cluster_owner())
                host.run(["pg_ctlcluster", str(cluster["major"]), "foundry", "reload"])
                result = _psql(host, registry, "SHOW archive_command;")
                require(_text(result) == command, "postgres_archive_pending", "Archive command publication awaits reconciliation.")
                cluster["archive_command"] = command
                journal["allocation"]["archive_command"] = command
                host.journal(registry)
                journal["archive_transition"]["phase"] = "complete"
                _save_cluster(host, journal)
            elif pending.get("phase") == "publishing":
                journal = _cluster_journal(host, registry)
                journal["allocation"]["archive_command"] = command
                journal["archive_transition"]["phase"] = "complete"
                _save_cluster(host, journal)
        _publish_hba(host, registry, running=True)
        return
    major = observed["major"]
    journal = _cluster_journal(host, registry)
    if journal is None:
        memory = host.paths.p("/proc/meminfo")
        found = re.search(r"^MemTotal:\s+(\d+) kB$", memory.read_text(), re.M)
        require(found and int(found[1]) >= 1536 * 1024, "postgres_capacity", "At least 1536 MiB host memory is required by the fixed Foundry service allocation.")
        allocation = _allocation(major, observed["port"], secrets.token_hex(32))
        allocation["state"] = "preparing"
        if shared_service_plan is not None:
            allocation["archive_command"] = _archive_plan(allocation, shared_service_plan)
        journal = {"host_id": registry["host_id"], "allocation": allocation, "phase": "reserved"}
        _save_cluster(host, journal)
    allocation = journal["allocation"]
    registry.setdefault("shared", {})["postgresql"] = dict(allocation)
    host.journal(registry)
    required = {f"postgresql-{major}": f"/usr/lib/postgresql/{major}/bin/postgres",
                f"postgresql-client-{major}": f"/usr/lib/postgresql/{major}/bin/psql",
                "postgresql-common": "/usr/bin/pg_createcluster", "openssl": "/usr/bin/openssl",
                "ca-certificates": "/usr/sbin/update-ca-certificates"}
    missing = [name for name, binary in required.items() if not host.paths.p(binary).is_file()]
    packages.install_missing(host, missing)
    _prepare_account(host, registry, journal)
    tls = prepare_tls(host, registry)
    config, data = host.paths.p(allocation["config_path"]), host.paths.p(allocation["data_path"])
    if journal["phase"] == "reserved":
        require(not config.exists() and not data.exists(), "postgres_ownership", "A cluster appeared after the Foundry reservation.")
        journal.update(phase="creating", creation_intent=True, start_attempted=False, start_succeeded=False)
        _save_cluster(host, journal)
    if journal["phase"] in {"creating", "recreating"}:
        if config.exists() or data.exists():
            _discard_initialization(host, registry, journal)
        _directory(host, host.paths.p(SOCKET), 0o755, cluster_owner(), cluster_owner())
        journal["phase"] = "creating"
        _save_cluster(host, journal)
        host.run(["pg_createcluster", str(major), "foundry", "--user", cluster_owner(),
                  "--group", cluster_owner(), "--datadir", allocation["data_path"],
                  "--port", str(allocation["port"]), "--socketdir", SOCKET, "--start-conf=manual",
                  "--", "--data-checksums", "--encoding=UTF8",
                  "--auth-local=peer", "--auth-host=scram-sha-256"], timeout=180)
        require(config.is_dir() and (data / "PG_VERSION").is_file()
                and (data / "PG_VERSION").read_text().strip() == str(major)
                and (config / "postgresql.conf").is_file(), "postgres_partial_install",
                "The claimed cluster creation is incomplete; retry the same allocation.")
        host.owned_write(_owner_path(host, allocation), json.dumps({"host_id": registry["host_id"], "ownership_token": allocation["ownership_token"]}) + "\n")
        journal["phase"] = "configuring"
        _save_cluster(host, journal)
    if journal["phase"] == "configuring":
        _directory(host, config, 0o750, cluster_owner(), cluster_owner())
        _directory(host, data, 0o700, cluster_owner(), cluster_owner())
        _directory(host, config / "conf.d", 0o755, cluster_owner(), cluster_owner())
        host.owned_write(config / "foundry.crt", tls["cert"].read_bytes(), owner=cluster_owner(), group=cluster_owner())
        host.owned_write(config / "foundry.key", tls["key"].read_bytes(), mode=0o600, owner=cluster_owner(), group=cluster_owner())
        host.owned_write(config / "conf.d/foundry.conf", _configuration(allocation),
                         owner=cluster_owner(), group=cluster_owner())
        _publish_hba(host, registry, running=False)
        host.owned_write(host.paths.p("/etc/systemd/system/" + allocation["unit"]), _unit(allocation))
        host.run(["systemctl", "daemon-reload"])
        journal = _cluster_journal(host, registry)
        journal["phase"] = "starting"
        _save_cluster(host, journal)
    cluster = registry["shared"]["postgresql"]
    active = _probe(host, ["systemctl", "is-active", "--quiet", allocation["unit"]])
    if not _ok(active):
        require(allocation["port"] not in _listeners(host), "postgres_port_conflict", "The reserved Foundry PostgreSQL port is occupied.")
        host.run(["systemctl", "enable", allocation["unit"]])
        journal["start_attempted"] = True
        _save_cluster(host, journal)
        host.run(["systemctl", "start", allocation["unit"]], timeout=120)
    journal["start_succeeded"] = True
    _save_cluster(host, journal)
    cluster["state"] = "ready"
    _verify_settings(host, registry)
    _publish_hba(host, registry, running=True)
    # HBA does not allow a shared TCP admin role. The first instance's actual
    # verify-full login records PostgreSQL trust separately in provision().
    journal = _cluster_journal(host, registry)
    journal["phase"] = "ready"
    journal["allocation"]["state"] = "ready"
    _save_cluster(host, journal)
    host.journal(registry)


def provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None:
    """Converge this UUID-owned database and role using supplied private credentials.

    Read names from entry['allocations']; verify ownership before reuse, preserve
    existing data/passwords, and journal completed_steps after verified creation.
    Credentials and their derived hashes never enter the public registry.
    """
    observed = inspect(host, registry)
    owned = _cluster(registry)
    require(observed.get("state") == "ready" and observed.get("owner") == "foundry"
            and observed.get("cluster") == "foundry" and observed.get("port") == owned["port"]
            and observed.get("ownership_token") == owned["ownership_token"],
            "postgres_ownership", "Foundry cluster ownership must be verified before provisioning.")
    _publish_hba(host, registry, running=True)
    _provision_database(host, registry, entry, credentials)


def _cluster(registry: dict) -> dict:
    """Require an explicit dedicated-cluster allocation before any connection."""
    cluster = registry.get("shared", {}).get("postgresql", {})
    require(_allocation_valid(cluster) and cluster.get("state") == "ready",
            "postgres_ownership", "Only the verified dedicated Foundry cluster may be used.")
    return cluster


def _psql(host: Host, registry: dict, sql: str, database: str = "postgres", transaction: bool = False):
    cluster = _cluster(registry)
    command = ["runuser", "-u", cluster_owner(), "--", f"/usr/lib/postgresql/{cluster['major']}/bin/psql",
               "-X", "-qAt", "--no-password", "-h", cluster["socket_directory"],
               "-p", str(cluster["port"]), "-d", database, "-v", "ON_ERROR_STOP=1", "-f", "-"]
    if transaction:
        command.append("--single-transaction")
    return host.run(command, input=sql, timeout=30)


def _sql(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def _probe(host: Host, args: list[str], **kwargs):
    try:
        return host.run(args, check=False, **kwargs)
    except (OSError, DeployError):
        return None


def _ok(result) -> bool:
    return result is not None and result.returncode == 0


def _text(result) -> str:
    if not _ok(result):
        return ""
    output = result.stdout or ""
    return (output.decode() if isinstance(output, bytes) else output).strip()


def _pending_path(host: Host, entry: dict) -> Path:
    directory = host.paths.p(instance_paths(entry["name"])["private"])
    host.check_control_directory(directory)
    return check_path(directory / "postgres.json", "file")


def _creation_pending(host: Host, entry: dict) -> bool:
    path = _pending_path(host, entry)
    if not path.exists():
        return False
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "postgres_ownership", "PostgreSQL journal must be root-owned and private.")
    value = read_json(path)
    require(isinstance(value, dict) and value.get("instance_id") == entry["profile"]["instance_id"]
            and type(value.get("database_create_pending")) is bool,
            "postgres_ownership", "PostgreSQL pending journal identity mismatch.")
    return value["database_create_pending"]


def _record_creation(host: Host, entry: dict, pending: bool) -> None:
    path = _pending_path(host, entry)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(not path.parent.stat().st_mode & 0o077, "postgres_ownership", "PostgreSQL journal directory must be private.")
    host.owned_write(path, json.dumps({"instance_id": entry["profile"]["instance_id"],
                                      "database_create_pending": pending}) + "\n", mode=0o600)


def _step(host: Host, registry: dict, entry: dict) -> None:
    if "database" not in entry["provisioning"]["completed_steps"]:
        entry["provisioning"]["completed_steps"].append("database")
        host.journal(registry, entry)


def _pg_resources(host: Host, registry: dict, entry: dict) -> dict:
    name = _sql(entry["allocations"]["database"])
    sql = f"""SELECT json_build_object(
'role', (SELECT json_build_object('marker', shobj_description(oid,'pg_authid'),
 'superuser', rolsuper, 'createdb', rolcreatedb, 'createrole', rolcreaterole,
 'replication', rolreplication, 'bypassrls', rolbypassrls, 'login', rolcanlogin,
 'memberships', (SELECT count(*) FROM pg_auth_members WHERE member=pg_roles.oid),
 'members', (SELECT count(*) FROM pg_auth_members WHERE roleid=pg_roles.oid))
 FROM pg_roles WHERE rolname={name}),
'database', (SELECT json_build_object('owner',pg_get_userbyid(datdba),
 'marker',shobj_description(oid,'pg_database'), 'public_access',
 EXISTS(SELECT 1 FROM aclexplode(COALESCE(datacl,acldefault('d',datdba))) a WHERE a.grantee=0 AND a.privilege_type IN ('CONNECT','TEMPORARY')),
 'foreign_access', EXISTS(SELECT 1 FROM aclexplode(COALESCE(datacl,acldefault('d',datdba))) a WHERE a.grantee NOT IN (0,datdba) AND a.privilege_type IN ('CONNECT','TEMPORARY','CREATE'))) FROM pg_database WHERE datname={name}));
"""
    return json.loads(_psql(host, registry, sql).stdout)


def _database_policy_ok(host: Host, registry: dict, entry: dict, database: dict) -> bool:
    if not database or database.get("public_access") is not False:
        return False
    sql = ("SELECT count(*) FROM pg_namespace n, LATERAL "
           "aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) a "
           "WHERE n.nspname='public' AND a.grantee=0 AND a.privilege_type='CREATE';")
    return _text(_psql(host, registry, sql, database=entry["allocations"]["database"])) == "0"


def _validate_pg_resources(entry: dict, value: dict, *, database_create_pending: bool = False) -> None:
    role, database = value["role"], value["database"]
    if role:
        if (role.get("marker") != marker(entry) or role.get("login") is not True or role.get("memberships") != 0 or role.get("members") != 0
                or any(role.get(key) for key in ("superuser", "createdb", "createrole", "replication", "bypassrls"))):
            raise DeployError("provision_conflict", "PostgreSQL role ownership or privilege mismatch")
    if database:
        # CREATE DATABASE cannot join a transaction. The durable pending claim
        # precedes CREATE; only its uniquely marked owner can complete the comment.
        pending = database_create_pending
        if (database.get("owner") != entry["allocations"]["database"] or database.get("foreign_access") is not False or not role or
                (database.get("marker") != marker(entry) and not (pending and database.get("marker") is None))):
            raise DeployError("provision_conflict", "PostgreSQL database ownership mismatch")


def _verify_pg_password(host: Host, registry: dict, entry: dict, credentials: dict, database: str | None = None) -> None:
    cluster = _cluster(registry)
    target = database or entry["allocations"]["database"]
    environment = {"PGPASSWORD": credentials["database_password"], "PGSSLMODE": "verify-full", "PGSSLROOTCERT": ROOTS,
                   "PGCONNECT_TIMEOUT": "5", "PGOPTIONS": "-c statement_timeout=5000"}

    def login(address, destination=target, **changes):
        return _probe(host, [f"/usr/lib/postgresql/{cluster['major']}/bin/psql", "-X", "-qAt", "--no-password",
                            "-h", address, "-p", str(cluster["port"]), "-U", entry["allocations"]["database"],
                            "-d", destination, "-c", "SELECT 1"], env=dict(environment, **changes), timeout=10)

    def positive(address):
        require(_text(login(address)) == "1", "provision_conflict",
                "Stored PostgreSQL credential failed verified TLS; restore credentials, never rotate automatically.")

    def denial(result, messages, code):
        error = (result.stderr or "").lower() if result is not None else ""
        require(result is not None and result.returncode != 0 and any(message in error for message in messages),
                code, "PostgreSQL denial did not prove the required authentication policy.")

    forbidden = {"postgres"}
    forbidden.update(item["allocations"]["database"] for item in registry.get("instances", {}).values()
                     if item["allocations"]["database"] != target and "database" in item["provisioning"]["completed_steps"])
    for address in ("localhost", "127.0.0.1", "::1"):
        positive(address)
        denial(login(address, PGPASSWORD="invalid-" + secrets.token_hex(32)),
               ("password authentication failed",), "postgres_password_bypass")
        denial(login(address, PGSSLMODE="disable"), ("pg_hba.conf rejects connection", "no pg_hba.conf entry"), "postgres_plaintext")
        for other_database in sorted(forbidden):
            denial(login(address, other_database), ("pg_hba.conf rejects connection", "no pg_hba.conf entry", "permission denied for database"),
                   "postgres_cross_database")
        positive(address)
    record_trust_probe(host, registry, "postgresql")


def _provision_database(host: Host, registry: dict, entry: dict, credentials: dict) -> None:
    """Converge only this marked role/database after dedicated-cluster discovery."""
    value = _pg_resources(host, registry, entry)
    _validate_pg_resources(entry, value, database_create_pending=_creation_pending(host, entry))
    name, ownership = _sql(entry["allocations"]["database"]), _sql(marker(entry))
    if not value["role"]:
        # CREATE and its ownership comment commit together. Password SQL goes
        # through stdin only; psql -q prevents successful SQL echoing.
        password = _sql(credentials["database_password"])
        sql = f"""SET LOCAL log_statement = 'none';
SET LOCAL log_min_error_statement = 'panic';
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L', {name}, {password}) \\gexec
SELECT format('COMMENT ON ROLE %I IS %L', {name}, {ownership}) \\gexec
"""
        _psql(host, registry, sql, transaction=True)
    if not value["database"]:
        _record_creation(host, entry, True)
        _psql(host, registry, f"SELECT format('CREATE DATABASE %I OWNER %I', {name}, {name}) \\gexec\n")
    if value["database"] and value["database"].get("marker") == marker(entry) and _database_policy_ok(host, registry, entry, value["database"]):
        _verify_pg_password(host, registry, entry, credentials)
        if _creation_pending(host, entry):
            _record_creation(host, entry, False)
        _step(host, registry, entry)
        return
    _psql(host, registry, f"SELECT format('COMMENT ON DATABASE %I IS %L', {name}, {ownership}) \\gexec\n"
               f"SELECT format('REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC', {name}) \\gexec\n", transaction=True)
    # PostgreSQL 15+ defaults public-schema ownership to pg_database_owner;
    # explicitly remove PUBLIC CREATE for installations whose templates differ.
    _psql(host, registry, "REVOKE CREATE ON SCHEMA public FROM PUBLIC;\n", database=entry["allocations"]["database"], transaction=True)
    _verify_pg_password(host, registry, entry, credentials)
    _record_creation(host, entry, False)
    _step(host, registry, entry)

