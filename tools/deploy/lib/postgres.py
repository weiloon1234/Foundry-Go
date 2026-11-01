"""Foundry-owned PostgreSQL cluster boundary. Implementation owner: lane F2."""
from __future__ import annotations

from host import Host
from pathlib import Path
import json
import secrets

from contracts import (DeployError, POSTGRES_PORT, check_path, instance_paths,
                       marker, read_json, require)

ROOTS = "/etc/ssl/certs/ca-certificates.crt"


def inspect(host: Host, registry: dict) -> dict:
    """Return read-only discovery of the dedicated <major>/foundry cluster.

    Inspect ownership, port, TLS, HBA, package metadata, and resumable preparation
    state. Foreign clusters are metadata-only and must never be authenticated to.
    """
    raise NotImplementedError("F2: dedicated PostgreSQL cluster discovery")


def prepare(host: Host, registry: dict) -> None:
    """Journal and prepare only the Foundry cluster, TLS, and HBA on port 55432.

    Use packages.install_missing when binaries are absent; retain conservative
    capacity settings in the public registry. Never alter/restart foreign main.
    """
    raise NotImplementedError("F2: dedicated PostgreSQL cluster preparation")


def provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None:
    """Converge this UUID-owned database and role using supplied private credentials.

    Read names from entry['allocations']; verify ownership before reuse, preserve
    existing data/passwords, and journal completed_steps after verified creation.
    Credentials and their derived hashes never enter the public registry.
    """
    observed = inspect(host, registry)
    owned = _cluster(registry)
    require(observed.get("state") == "ready" and observed.get("owner") == "foundry"
            and observed.get("cluster") == "foundry" and observed.get("port") == POSTGRES_PORT
            and observed.get("ownership_token") == owned["ownership_token"],
            "postgres_ownership", "Foundry cluster ownership must be verified before provisioning.")
    _provision_database(host, registry, entry, credentials)


def _cluster(registry: dict) -> dict:
    """Require an explicit dedicated-cluster allocation before any connection."""
    cluster = registry.get("shared", {}).get("postgresql", {})
    require(cluster.get("owner") == "foundry" and cluster.get("cluster") == "foundry"
            and cluster.get("state") == "ready" and cluster.get("port") == POSTGRES_PORT
            and cluster.get("major") in (16, 18) and bool(cluster.get("ownership_token"))
            and cluster.get("unit") == f"postgresql@{cluster.get('major')}-foundry.service",
            "postgres_ownership", "Only the verified dedicated Foundry cluster may be used.")
    return cluster


def _psql(host: Host, registry: dict, sql: str, database: str = "postgres", transaction: bool = False):
    _cluster(registry)
    command = ["runuser", "-u", "postgres", "--", "psql", "-X", "-qAt", "-h", "/var/run/postgresql",
               "-p", str(POSTGRES_PORT), "-d", database, "-v", "ON_ERROR_STOP=1", "-f", "-"]
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
    _cluster(registry)
    target = database or entry["allocations"]["database"]
    result = _probe(host, ["psql", "-X", "-qAt", "-h", "localhost", "-p", str(POSTGRES_PORT), "-U", entry["allocations"]["database"],
                           "-d", target, "-c", "SELECT 1"],
                    env={"PGPASSWORD": credentials["database_password"], "PGSSLMODE": "verify-full", "PGSSLROOTCERT": ROOTS}, timeout=10)
    if _text(result) != "1":
        raise DeployError("provision_conflict", "stored PostgreSQL credential failed; restore credentials, never rotate automatically")
    # A successful login alone is not proof that pg_hba requires a password.
    invalid = _probe(host, ["psql", "-X", "-qAt", "-h", "localhost", "-p", str(POSTGRES_PORT), "-U", entry["allocations"]["database"],
                           "-d", target, "-c", "SELECT 1"],
                    env={"PGPASSWORD": "invalid-" + secrets.token_hex(32), "PGSSLMODE": "verify-full", "PGSSLROOTCERT": ROOTS}, timeout=10)
    if _ok(invalid):
        raise DeployError("postgres_password_bypass", "PostgreSQL accepts an invalid instance password; secure host authentication before bootstrap.")


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

