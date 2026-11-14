#!/usr/bin/env python3
"""Feature recovery phases for the disposable host harness after integration.

RECOVERY_HANDOFF.md owns runner hooks. The source retains its age identity;
only the decrypted configuration archive and exact release cross to the target.
Missing dispatcher or lifecycle seams fail this scenario, never skip it.
"""
from contextlib import ExitStack
import copy
import importlib.util
import io
import json
from pathlib import Path
import shutil
import sys
import tarfile
import time
from unittest.mock import patch
import uuid

sys.dont_write_bytecode = True
TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS / "lib"))
from acceptance_support import WORK, action, guard, postgres_settings, psql, request, run
from acceptance_backup import rclone, s3_settings
from acceptance_recover import table_snapshot
import acceptance_feature as feature
import acceptance_host as shared
import acceptance_network as network
import backup
from contracts import (DeployError, ReleaseDeclaration, atomic_json, canonical_digest,
                       digest, read_json)
import edge
import feature_lifecycle
from host import Host
import listeners
import recover
import services
from topology import parse_profile, parse_topology

EXPORT = WORK / "recover-export"
TARGET_ADDRESSES = ("203.0.113.11", "2001:db8:42::11")


def state_archive(source, destination, state, version):
    """Reissue the fixture with a different state policy and a fresh release ID."""
    assert state in {"disposable", "local-unprotected"}
    with tarfile.open(source, "r:gz") as incoming:
        files = {item.name: (item.mode, incoming.extractfile(item).read())
                 for item in incoming if item.isfile()}
    _, raw = files.pop("manifest.json")
    manifest = json.loads(raw)
    old = manifest["version"]
    assert version != old
    def rename(name):
        prefix = "private-source-maps/" + old + "/"
        return "private-source-maps/" + version + "/" + name[len(prefix):] if name.startswith(prefix) else name
    files = {rename(name): value for name, value in files.items()}
    declaration = manifest["deployment"]
    relay = next(role for role in declaration["topology"]["roles"] if role["name"] == "relay")
    relay["state"] = {"kind": state, **({"acknowledged": True} if state == "local-unprotected" else {})}
    topology = parse_topology(declaration["topology"])
    declaration["topology_sha256"] = topology.sha256
    for member in declaration["inventory"]:
        member["path"] = rename(member["path"])
    manifest.update(version=version, topology_sha256=topology.sha256,
                    deployment=ReleaseDeclaration.parse(declaration).projection(),
                    files={rename(name): checksum for name, checksum in manifest["files"].items()})
    files["manifest.json"] = (0o644, json.dumps(manifest).encode())
    with tarfile.open(destination, "w:gz") as outgoing:
        for name, (mode, data) in sorted(files.items()):
            item = tarfile.TarInfo(name)
            item.mode, item.size = mode, len(data)
            outgoing.addfile(item, io.BytesIO(data))
    return manifest


def assert_coverage(receipt, *, protected):
    assert receipt["status"] == "complete", "Supported backup components did not complete"
    assert receipt["pgbackrest_label"] and receipt["database_point"]["pgbackrest_label"] == receipt["pgbackrest_label"]
    keys = receipt["key_archive"]
    assert keys["status"] == "complete" and keys["object_key"]
    assert keys["deployment_sha256"] == receipt["deployment_sha256"]
    assert set(keys["coverage"]["supported_components"]) == {"encrypted_configuration", "selected_deployment"}
    assert keys["coverage"]["complete_instance_coverage"] is False, "Keys alone cannot claim database coverage"
    coverage = receipt["coverage"]
    assert set(coverage["supported_components"]) == {"postgresql", "encrypted_configuration", "selected_deployment"}
    assert coverage["complete_instance_coverage"] is protected
    state = "disposable" if protected else "local-unprotected"
    assert coverage["service_state"] == [{"role": "relay", "kind": state}]
    expected = [] if protected else [{"code": "service_state_unprotected", "role": "relay"}]
    assert coverage["findings"] == keys["coverage"]["findings"] == expected


def replacement_binding(metadata, target_id, operation):
    return {"schema": 1, "operation_id": operation,
            "source_instance_id": metadata["profile"]["instance_id"], "source_host_id": metadata["host_id"],
            "target_host_id": target_id, "deployment_sha256": canonical_digest(metadata["feature_recovery"]["deployment"]),
            "dns_cutover": "external_pending", "observations": [], "listeners": [
                {"name": name, "hostname": binding["dns"]["hostname"], "transport": "tcp",
                 "ports": binding["ports"], "dns_mode": "external",
                 "old_addresses": binding["dns"]["public_addresses"], "replacement_addresses": list(TARGET_ADDRESSES),
                 "source_binding_sha256": canonical_digest(binding)}
                for name, binding in sorted(metadata["profile"]["listeners"].items())]}


def stopped_units(inventory):
    result = {}
    for role, unit in inventory.units.items():
        observed = run(["systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState", "-p", "MainPID", "-p", "ControlPID",
                        "-p", "InvocationID", "-p", "ExecMainStartTimestampMonotonic", "-p", "NRestarts"]).stdout
        values = dict(line.split("=", 1) for line in observed.splitlines() if "=" in line)
        assert values["LoadState"] == "loaded", "Prepared workload unit is missing"
        assert values["ActiveState"] == "inactive" and values["MainPID"] == values["ControlPID"] == "0", "Workload is not stopped"
        result[role] = values
    return result


def drill(host, project, profile, entry, receipt):
    _, _, inventory, _ = feature_lifecycle.load(host, entry)
    before = stopped_units(inventory)
    sockets = {row.socket for row in listeners.observe_sockets(host)}
    feature.peer_reachable(host, opened=False)
    result = action("restore-drill", request(project, profile, backup_id=receipt["backup_id"], retain=True), timeout=3600)
    assert result["status"] == "complete" and result["retained"] is True
    assert all(result["verification"][name] is False for name in
               ("custom_services_started", "network_listeners", "complete_instance_coverage"))
    try:
        restored = result["postgres"]
        assert restored["host"].startswith("/") and restored["sslmode"] == "disable"
        sql = run(["runuser", "-u", restored["user"], "--", "psql", "-X", "-qAt", "--no-password",
                   "-v", "ON_ERROR_STOP=1", "-h", restored["host"], "-p", str(restored["port"]),
                   "-d", restored["database"]], input="SELECT 1; SHOW listen_addresses; SELECT pg_is_in_recovery();").stdout
        assert sql.splitlines() == ["1", "", "t"], "Drill must recover over a Unix socket without TCP"
        assert stopped_units(inventory) == before, "Drill started or restarted a workload"
        assert {row.socket for row in listeners.observe_sockets(host)} <= sockets, "Drill opened a network listener"
        feature.peer_reachable(host, opened=False)
    finally:
        # Retain the private drill data for diagnostics; stop its owned processes.
        backup._stop_drill(host, result["unit"])
        backup._stop_drill(host, result["restore_unit"])
    assert stopped_units(inventory) == before
    print("PASS feature restore drill: stopped workloads unchanged, no new sockets, Unix-only PostgreSQL and peer TCP closed", flush=True)


def source():
    host = Host()
    original = shared.WORK / "feature.tar.gz"
    with tarfile.open(original, "r:gz") as archive:
        manifest = json.load(archive.extractfile("manifest.json"))
    profile = manifest["deployment"]["profile"]
    project = {"schema": 1, "id": manifest["project_id"], "slug": manifest["instance"].removesuffix("-" + manifest["environment"])}
    _, entry = host.require_identity(project, profile)
    assert not EXPORT.exists(), "Use a fresh acceptance run"
    EXPORT.mkdir(mode=0o700)
    with ExitStack() as cleanup:
        network.prepare_peer(host, cleanup.callback)
        # Exercise both receipts through the real archive/deploy/backup owners.
        selected = None
        for state in ("local-unprotected", "disposable"):
            archive = shared.WORK / ("feature-" + state + ".tar.gz")
            version = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + uuid.uuid4().hex
            state_archive(original, archive, state, version)
            activation = request(project, profile, archive=str(archive), archive_sha256=digest(archive),
                                 operation_id=str(uuid.uuid4()))
            review = action("release-plan", activation)
            assert review["ok"]
            assert action("activate", {**activation, "plan_token": review["plan_token"]}, timeout=1800)["version"] == version
            action("stop", request(project, profile))
            _, entry = host.require_identity(project, profile)
            _, _, inventory, _ = feature_lifecycle.load(host, entry)
            stopped_units(inventory)
            if state == "local-unprotected":
                receipt = action("backup-run", request(project, profile, kind="full"), timeout=3600)
                assert_coverage(receipt, protected=False)
            else:
                selected = archive
        # Out-of-band systemd changes deliberately leave cached selection alone.
        # HTTP depends on relay; relay alone is a valid enabled graph.
        run(["systemctl", "disable", inventory.units["http"]])
        run(["systemctl", "enable", inventory.units["relay"]])
        assert set(services.enabled_units(host, inventory.units)) == {"relay"}
        run(["systemctl", "disable", "--now", "foundry-backup-full-cluster.timer", "foundry-backup-diff-cluster.timer"])
        for unit in ("foundry-backup-full-cluster.service", "foundry-backup-diff-cluster.service"):
            assert run(["systemctl", "show", unit, "-p", "ActiveState", "--value"]).stdout.strip() == "inactive"
        pg = postgres_settings(host, entry)
        table = "acceptance_feature_recovery_" + uuid.uuid4().hex
        psql(pg, f"CREATE TABLE {table} (id integer PRIMARY KEY, value text NOT NULL); "
                 f"INSERT INTO {table} SELECT n, 'feature-recovery-' || n FROM generate_series(1,25) n;")
        snapshot = table_snapshot(pg)
        receipt = action("backup-run", request(project, profile, kind="full"), timeout=3600)
        assert_coverage(receipt, protected=True)
        backup._backrest(host, ["check"], timeout=300)
        drill(host, project, profile, entry, receipt)
        assert table_snapshot(pg) == snapshot, "Backup/drill changed live data"
        encrypted = EXPORT / "keys.age"
        settings = s3_settings()
        rclone(settings, "copyto", f"acceptance:{settings['backup_bucket']}/{receipt['key_archive']['object_key']}", str(encrypted))
        assert encrypted.read_bytes().startswith(b"age-encryption.org/v1\n")
        run(["age", "--decrypt", "-i", str(shared.WORK / "age-identity.txt"),
             "-o", str(EXPORT / "recovery.tar"), str(encrypted)])
        encrypted.unlink()
        files = recover.read_archive(EXPORT / "recovery.tar")
        recovered = recover.archive_configuration(project, profile, files, entry["topology"])
        metadata = recovered["metadata"]
        archived = metadata["feature_recovery"]
        assert archived["selection"]["enabled_roles"] == ["relay"], "Backup ignored live systemd enablement"
        assert archived["selection"]["maintenance_holds"] == [], "Fixture must not start with a maintenance hold"
        assert canonical_digest(archived["deployment"]) == receipt["deployment_sha256"]
        assert archived["declaration"] == read_manifest(selected)["deployment"]
        shutil.copyfile(selected, EXPORT / "release.tar.gz")
        atomic_json(EXPORT / "source.json", {"project": project, "profile": profile, "topology": entry["topology"],
                    "snapshot": snapshot, "table": table, "receipt": receipt}, mode=0o600)
        assert {path.name for path in EXPORT.iterdir()} == {"source.json", "release.tar.gz", "recovery.tar"}
    print("PASS feature backup: component success vs complete coverage, local-unprotected finding, actual enabled set and exact descriptor archived", flush=True)


def read_manifest(path):
    with tarfile.open(path, "r:gz") as archive:
        return json.load(archive.extractfile("manifest.json"))


def manager():
    spec = importlib.util.spec_from_file_location("acceptance_feature_recovery_manage", TOOLS / "server/manage.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def target_plan():
    host = Host()
    assert host.read_registry(missing=True) is None
    source = read_json(EXPORT / "source.json")
    metadata = json.loads(recover.read_archive(EXPORT / "recovery.tar")["recovery/instance.json"])
    profile = copy.deepcopy(source["profile"])
    profile["host"].update(address="feature-takeover.acceptance.invalid", host_id=str(uuid.uuid4()))
    operation = str(uuid.uuid4())
    payload = request(source["project"], profile, operation_id=operation, mode="takeover", topology=source["topology"],
                      recovery_archive=str(EXPORT / "recovery.tar"), archive_sha256=digest(EXPORT / "release.tar.gz"),
                      recovery_public_binding=replacement_binding(metadata, profile["host"]["host_id"], operation))
    manage = manager()
    packages = run(["dpkg-query", "-W", "-f=${binary:Package}\t${Version}\t${db:Status-Status}\n"]).stdout
    sockets = {row.socket for row in listeners.observe_sockets(host)}
    plan = manage.execute(host, "recover-plan", payload)
    assert plan["ok"] and plan["recovery_roles"] == plan["recovery_enabled_roles"] == ["relay"]
    assert plan["recovery_maintenance_holds"] == []
    assert plan["recovery_public_binding"]["deployment_sha256"] == source["receipt"]["deployment_sha256"]
    apply = {**payload, "archive": str(EXPORT / "release.tar.gz"), "plan_token": plan["plan_token"]}
    try:
        manage.execute(host, "recover-apply", {**apply, "old_server_gone": False})
    except DeployError as error:
        assert error.code == "old_server_confirmation", "Wrong refusal before stop confirmation"
    else:
        raise AssertionError("Takeover accepted an unconfirmed source stop")
    assert host.read_registry(missing=True) is None and not Path(recover.JOURNAL).parent.exists()
    assert not Path("/usr/bin/pgbackrest").exists() and not Path("/usr/bin/jq").exists(), "Use the empty base image"
    assert run(["dpkg-query", "-W", "-f=${binary:Package}\t${Version}\t${db:Status-Status}\n"]).stdout == packages
    assert {row.socket for row in listeners.observe_sockets(host)} == sockets, "Unconfirmed takeover opened a port"
    atomic_json(EXPORT / "apply.json", apply, mode=0o600)
    print("PASS feature target plan: relay enabled, HTTP disabled, unconfirmed takeover refused without provisioning or listeners", flush=True)


def target_peer(host, cleanup):
    network.prepare_peer(host, cleanup.callback)
    for index, family in enumerate((4, 6)):
        prefix = "/24" if family == 4 else "/64"
        host.run(["ip", "-" + str(family), "addr", "del", network.ADDRESSES[index] + prefix, "dev", "fdy-host"])
        host.run(["ip", "-" + str(family), "addr", "add", TARGET_ADDRESSES[index] + prefix, "dev", "fdy-host",
                  *([] if family == 4 else ["nodad"])])
    assert host.run(["readlink", "/proc/self/ns/net"]).stdout != host.run(
        ["ip", "netns", "exec", network.PEER, "readlink", "/proc/self/ns/net"]).stdout


def probe_target(host, *, opened):
    for address in TARGET_ADDRESSES:
        response = host.run(["ip", "netns", "exec", network.PEER, "python3", "-I", "-c", feature.PEER_PROBE,
                             address, str(feature.PORT), "yes" if opened else "no"], timeout=8, check=False)
        assert (response.returncode, response.stdout) == ((0, feature.RELAY_BODY) if opened else (2, "")), "Replacement peer TCP disagrees with admission"


def takeover():
    # Only the outer runner can prove the other container stopped; require its
    # explicit handoff after docker stop + inspect, never infer it from a timeout.
    assert sys.argv[2:] == ["--old-server-stopped"], "Runner must confirm the old container is stopped"
    host = Host()
    assert host.read_registry(missing=True) is None
    source, payload = read_json(EXPORT / "source.json"), read_json(EXPORT / "apply.json")
    metadata = json.loads(recover.read_archive(EXPORT / "recovery.tar")["recovery/instance.json"])
    archived_inventory = backup.parse_feature_snapshot(metadata["feature_recovery"], metadata)
    prepared, activated = [], []
    prepare, activate = feature_lifecycle.prepare_recovery, feature_lifecycle.activate_recovery
    with ExitStack() as cleanup:
        target_peer(host, cleanup)
        network.prepare_firewall(host)
        probe_target(host, opened=False)
        def observed_prepare(host, registry, entry, journal, snapshot, *args):
            assert journal["old_server_stopped_confirmed_at"]
            assert snapshot == metadata["feature_recovery"]
            # Same synthetic CA boundary as acceptance_recover; no public issuer.
            directory = Path("/etc/foundry/acceptance-ca")
            directory.mkdir(mode=0o700, exist_ok=True)
            for name in ("ca.crt", "ca.key"):
                shutil.copyfile(WORK / name, directory / name)
                (directory / name).chmod(0o600)
            result = prepare(host, registry, entry, journal, snapshot, *args)
            stopped_units(archived_inventory)
            assert set(services.enabled_units(host, archived_inventory.units)) == {"relay"}
            probe_target(host, opened=False)
            prepared.append(True)
            return result
        def observed_activate(host, registry, entry, journal):
            assert prepared and journal["old_server_stopped_confirmed_at"]
            assert table_snapshot(postgres_settings(host, entry)) == source["snapshot"], "Recovered table counts/hashes differ before startup"
            result = activate(host, registry, entry, journal)
            activated.append(True)
            return result
        # Wrappers assert boundaries, then delegate unchanged to real lane A code.
        with patch.object(feature_lifecycle, "prepare_recovery", side_effect=observed_prepare), \
                patch.object(feature_lifecycle, "activate_recovery", side_effect=observed_activate):
            result = manager().execute(host, "recover-apply", {**payload, "old_server_gone": True})
        assert prepared and activated and result["ok"] and result["host_ready"]
        assert result["version"] == metadata["feature_recovery"]["deployment"]["release"]
        profile = result["profile_update"]
        assert profile["listeners"]["relay.control"]["dns"]["public_addresses"] == list(TARGET_ADDRESSES)
        _, entry = host.require_identity(source["project"], profile)
        _, selection, inventory, launch = feature_lifecycle.load(host, entry)
        assert launch["declaration"] == metadata["feature_recovery"]["declaration"]
        assert read_json(Path(recover.JOURNAL).with_name("source-deployment.json")) == metadata["feature_recovery"]
        assert set(services.enabled_units(host, inventory.units)) == {"relay"} and selection.maintenance_holds == ()
        assert run(["systemctl", "is-active", inventory.units["relay"]]).stdout.strip() == "active"
        disabled = run(["systemctl", "show", inventory.units["http"], "-p", "ActiveState", "-p", "MainPID",
                        "-p", "ExecMainStartTimestampMonotonic"]).stdout
        assert dict(line.split("=", 1) for line in disabled.splitlines()) == {
            "ActiveState": "inactive", "MainPID": "0", "ExecMainStartTimestampMonotonic": "0"}, "Disabled HTTP started during recovery"
        assert run(["dpkg-query", "-W", "-f=${db:Status-Status}", "jq"]).stdout == "installed"
        run(["/usr/bin/jq", "--version"])
        for spec in inventory.specs.values():
            path = Path(spec.executable)
            assert path.is_file() and path.stat().st_mode & 0o111 and digest(path) == spec.executable_sha256, "Selected executable was not restored exactly"
        relay = next(unit for unit in metadata["feature_recovery"]["deployment"]["units"] if unit["identity"]["role"] == "relay")
        assert inventory.specs["relay"].executable_sha256 == relay["executable_sha256"]
        assert services.wait_ready(host, inventory, "relay", time.monotonic() + 90)
        placed = parse_profile(profile, inventory.topology)
        expected = edge.stream_projection(placed, inventory.resolution, None, topology=inventory.topology)
        assert expected
        edge.observe_projection(host, profile["instance_id"], ("relay",), expected=expected)
        probe_target(host, opened=True)
        claims = listeners.public_claims(placed)
        observed = tuple(row for row in listeners.observe_sockets(host) if any(listeners.conflicts(row.socket, claim.socket) for claim in claims))
        assert observed and not listeners.ownership_findings(claims, observed,
            {"nginx": listeners.process_members(host, "nginx.service")}, edge_verified=True)
        assert psql(postgres_settings(host, entry), f"SELECT count(*) FROM {source['table']};").stdout.strip() == "25"
        assert_coverage(result["backup"], protected=True)
    print("PASS feature takeover: confirmed stop, exact descriptor/data, packages/artifacts, enabled relay only, disabled HTTP never started, dual-family TCP from peer", flush=True)


if __name__ == "__main__":
    guard()
    command = sys.argv[1:]
    if command == ["source"]:
        source()
    elif command == ["target-plan"]:
        target_plan()
    elif command == ["takeover", "--old-server-stopped"]:
        takeover()
    else:
        raise SystemExit("Expected source, target-plan or takeover --old-server-stopped")
