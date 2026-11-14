#!/usr/bin/env python3
"""Opt-in host-capability scenario inside the existing disposable harness.

Use the ordinary deploy-acceptance archive. The artifact is a second, pinned copy
of that fixture's Linux artisan binary; no artifact network download is needed.
Lane B's archive verification and publication remain production code paths.
"""
from contextlib import ExitStack
from dataclasses import asdict
import argparse
import copy
import hashlib
import io
import ipaddress
import json
from pathlib import Path
import pwd
import secrets
import sys
import tarfile
import tomllib
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import artifacts
from contracts import ReleaseDeclaration, digest, read_json
from host import Host
from topology import parse_topology, parse_profile
from acceptance_support import LAUNCHER, action, guard, request, run
import acceptance_host as shared
import acceptance_network as network
import edge
import firewall
import listeners
import services

PORT = 8554
RELAY_BODY = "feature-relay-ready"
PEER_PROBE = r'''
import http.client, socket, sys
address, port, opened = sys.argv[1:]
try:
    connection = socket.create_connection((address, int(port)), timeout=2)
except OSError:
    sys.exit(2)
with connection:
    connection.settimeout(3)
    if opened == "yes":
        connection.sendall(b"GET /ready HTTP/1.1\r\nHost: relay.acceptance.invalid\r\nConnection: close\r\n\r\n")
        response = http.client.HTTPResponse(connection)
        response.begin()
        assert response.status == 200
        sys.stdout.buffer.write(response.read(128))
    else:
        print("CONNECTED")
'''


def peer_reachable(host, *, opened):
    for address in network.ADDRESSES:
        response = host.run(["ip", "netns", "exec", network.PEER, "python3", "-I", "-c", PEER_PROBE,
                             address, str(PORT), "yes" if opened else "no"], timeout=8, check=False)
        if opened:
            assert response.returncode == 0 and response.stdout == RELAY_BODY, "Peer did not receive the relay's bytes"
        else:
            assert response.returncode == 2 and not response.stdout, "Public TCP connection was not refused or timed out"


def public_observation(host, topology, profile, *, inventory=None, opened=False):
    placed = parse_profile(profile, topology)
    claims = listeners.public_claims(placed)
    observation = firewall.observe(host, placed.instance_id, claims, bindings=placed.listeners, opened=opened)
    assert observation.closed is not opened
    expected = {}
    if opened:
        backend = next(e for e in inventory.resolution.endpoints if (e.role, e.listener) == ("relay", "control"))
        assert ipaddress.ip_address(backend.address).is_loopback and backend.port != PORT
        expected = edge.stream_projection(placed, inventory.resolution, None, topology=topology)
        assert expected and all("proxy_pass " + backend.address + ":" + str(backend.port) + ";" in value for value in expected.values())
        expected["http-open.conf"] = '"" 1;\n'
    edge.observe_projection(host, placed.instance_id, ("http", "relay"), expected=expected)
    if opened:
        observed = tuple(s for s in listeners.observe_sockets(host) if any(listeners.conflicts(s.socket, claim.socket) for claim in claims))
        assert not listeners.ownership_findings(claims, observed,
            {"nginx": listeners.process_members(host, "nginx.service")}, edge_verified=True), "Public sockets must belong to nginx"
    peer_reachable(host, opened=opened)


def assert_listener_report(result, entry):
    instances = [instance for project in result["projects"] for instance in project["instances"]]
    instance = next(instance for instance in instances if instance["name"] == entry["name"])
    value = instance["network"]
    rows = [row for row in value["listeners"] if row["port"] == PORT]
    assert len(rows) == 2 and {row["address"] for row in rows} == set(network.ADDRESSES)
    assert all((row["role"], row["listener"], row["expected_owner"], row["tls"], row["authentication"]) ==
               ("relay", "control", "nginx", "none", True) for row in rows)
    assert not any(f["code"] in {"listener_owner_unverified", "nginx_projection_unverified", "declared_listener_absent"}
                   for f in value["findings"])


def assert_secret_template(host, inventory, entry, secret):
    paths = services.ServicePaths.for_unit(inventory.specs["relay"].identity, inventory.resolution.id)
    config = Path(paths.config("private", "rendered-template", "relay.yaml"))
    account = pwd.getpwnam(entry["name"])
    assert config.is_file(), "Prepared secret template is missing"
    assert (config.stat().st_uid, config.stat().st_gid, config.stat().st_mode & 0o777) == (0, account.pw_gid, 0o640)
    assert json.loads(config.read_text().removeprefix("token: ")) == secret, "Prepared template differs from private input"
    baseline = Path(paths.base) / "current/baselines/private"
    assert baseline.read_bytes() == config.read_bytes()
    assert (baseline.stat().st_uid, baseline.stat().st_gid, baseline.stat().st_mode & 0o777) == (0, account.pw_gid, 0o640)
    assert run(["runuser", "-u", entry["name"], "--", "test", "-r", str(config)], check=False).returncode == 0
    # Check every workload process, not merely MainPID, without printing bytes.
    for unit in inventory.units.values():
        members = listeners.process_members(host, unit)
        assert members, "Workload cgroup has no processes"
        for pid in members:
            for leaf in ("cmdline", "environ"):
                assert secret.encode() not in Path(f"/proc/{pid}/{leaf}").read_bytes(), "Secret leaked into a process interface"
        assert secret not in run(["systemctl", "cat", unit]).stdout, "Secret leaked into a unit"
    journal = run(["journalctl", "--boot", "--no-pager", "--output=cat"], timeout=30).stdout
    assert secret not in journal and json.dumps(secret)[1:-1] not in journal, "Secret leaked into the journal"
    assert secret not in json.dumps(host.read_registry()), "Secret leaked into public state"
    return config.resolve()


def feature_archive(source, destination, host_id):
    with tarfile.open(source, "r:gz") as incoming:
        files = {item.name: incoming.extractfile(item).read() for item in incoming if item.isfile()}
    manifest = json.loads(files.pop("manifest.json"))
    project = {"schema": 1, "id": manifest["project_id"], "slug": manifest["instance"].removesuffix("-" + manifest["environment"])}
    profile = read_json(Path("/verification/foundry-go/tests/fixtures/deploy-acceptance/deploy/environments/acceptance.json"))
    profile.update(schema=3, architecture=manifest["architecture"], instance_id=manifest["instance_id"],
                   placement={"http": 1, "relay": 1}, listeners={})
    # Synthetic certificates are issued only for the disposable acceptance zone.
    profile["domain"] = f"{project['slug']}-{profile['environment']}.acceptance.invalid"
    profile["host"]["host_id"] = host_id
    profile["listeners"] = {"relay.control": {"ports": [PORT],
        "source_cidrs": [network.PEER_ADDRESSES[0] + "/32", network.PEER_ADDRESSES[1] + "/128"],
        "dns": {"mode": "external", "hostname": "relay." + profile["domain"],
                "public_addresses": list(network.ADDRESSES), "proxied": False}}}
    checksum = hashlib.sha256(files["artisan"]).hexdigest()
    platform = "linux-" + profile["architecture"]
    topology = parse_topology({"schema": 2, "roles": [
        {"name": "http", "kind": "builtin", "requires": ["relay"], "packages": ["jq"],
         "bindings": {"relay": {"endpoint": {"role": "relay", "listener": "control", "scheme": "http"}},
                      "checksum": {"executable": {"package": "jq", "path": "/usr/bin/jq"}}}},
        {"name": "relay", "kind": "executable", "executable": {"artifact": "relay", "entry": "server"},
         "arguments": [{"literal": "feature:relay"}, {"listener_address": "control"}, {"listener_port": "control"}],
         "secret_files": {"TOKEN_FILE": "relay-template-token"},
         "configs": {"private": {"source": "deploy/files/relay.yaml", "mode": "rendered-template",
                                 "format": "yaml", "path": "relay.yaml", "directory_environment": "XDG_CONFIG_HOME"}},
         "listeners": {"control": {"transport": "tcp", "bind": "public", "tls": "none", "authentication": True}},
         "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200, "body": "feature-relay-ready"},
         "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
         "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
         "maintenance": "continue", "state": {"kind": "disposable"}}],
        "secrets": {"relay-template-token": {"prompt": "Relay template token"}},
        "artifacts": {"relay": {"version": "fixture-1", "format": "file", "executables": {"server": "relay"},
            "platforms": {platform: {"url": "https://acceptance.invalid/relay", "sha256": checksum}}}}})
    profile = parse_profile(profile, topology).projection()
    payload_path = "deploy/artifacts/relay/" + checksum + ".bin"
    files[payload_path] = files["artisan"]
    files["deploy/files/relay.yaml"] = b"token: {{secret.TOKEN_FILE}}\n"
    payload = destination.parent / "relay-fixture.bin"
    payload.write_bytes(files["artisan"])
    members = artifacts.inspect_payload(dict(topology.artifacts)["relay"], platform, payload, artifacts.ExpansionBudget())
    payload.unlink()
    kinds = artifacts.declared_release_files(topology, (), (payload_path,))
    declaration = ReleaseDeclaration.parse({"schema": 2, "topology": topology.projection(), "profile": profile,
        "topology_sha256": topology.sha256, "application_executables": {},
        "inventory": [{"path": name, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data),
                       "kind": artifacts.release_member_kind(name, kinds)} for name, data in sorted(files.items())],
        "artifacts": [{"artifact": "relay", "platform": platform, "path": payload_path, "sha256": checksum,
                       "members": [asdict(member) for member in members]}]})
    manifest.update(schema=2, deployment=declaration.projection(), topology_sha256=topology.sha256,
                    files={name: hashlib.sha256(data).hexdigest() for name, data in files.items()})
    files["manifest.json"] = json.dumps(manifest).encode()
    with tarfile.open(destination, "w:gz") as outgoing:
        for name, data in sorted(files.items()):
            item = tarfile.TarInfo(name)
            item.size, item.mode = len(data), 0o755 if name == "artisan" else 0o644
            outgoing.addfile(item, io.BytesIO(data))
    disks = copy.deepcopy(tomllib.loads(files["config/deploy.toml"].decode())["app"]["services"]["storage"]["disks"])
    return project, profile, topology, disks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    args = parser.parse_args()
    guard()
    host = Host()
    assert host.read_registry(missing=True) is None, "Feature scenario requires a fresh disposable host"
    shared.WORK.mkdir(mode=0o700, exist_ok=True)
    archive = shared.WORK / "feature.tar.gz"
    project, profile, topology, disks = feature_archive(args.archive, archive, shared.HOST_ID)
    shared.private_json(shared.WORK / "topology.json", {**topology.projection(), "sha256": topology.sha256})
    private = shared.bootstrap_secrets(disks)
    secret = secrets.token_urlsafe(32) + '"#;]雪'
    private.setdefault("configuration", {})["FOUNDRY_SERVICE_SECRET__RELAY_TEMPLATE_TOKEN"] = secret
    with ExitStack() as cleanup:
        network.prepare_peer(host, cleanup.callback)
        original = host.run(["readlink", "/proc/self/ns/net"]).stdout.strip()
        peer = host.run(["ip", "netns", "exec", network.PEER, "readlink", "/proc/self/ns/net"]).stdout.strip()
        assert original and peer and original != peer, "Probe must run outside the host network namespace"
        network.prepare_firewall(host)
        claims = listeners.public_claims(parse_profile(profile, topology))
        assert firewall.observe(host, profile["instance_id"], claims, integrated=False).closed
        peer_reachable(host, opened=False)
        shared.prepare(host, (project, profile), private)
        public_observation(host, topology, profile)
        activation = request(project, profile, archive=str(archive), archive_sha256=digest(archive), operation_id=str(uuid.uuid4()))
        review = action("release-plan", activation)
        assert review["ok"]
        result = action("activate", {**activation, "plan_token": review["plan_token"]})
        assert result["ok"]
        from feature_lifecycle import load
        _, entry = host.require_identity(project, profile)
        _, _, inventory, _ = load(host, entry)
        for unit in inventory.units.values():
            assert run(["systemctl", "is-active", unit]).stdout.strip() == "active"
        first_config = assert_secret_template(host, inventory, entry, secret)
        # HTTP readiness calls the relay using deployment.LoadBindings, proving app delivery.
        import services
        import time
        assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        reported = json.loads(run([LAUNCHER, "report"], input=json.dumps(request(project, profile))).stdout)
        assert_listener_report(reported, entry)
        first = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
        assert action("restart", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
        second = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
        assert first.strip() and second.strip() and first != second
        assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
        _, entry = host.require_identity(project, profile)
        _, _, inventory, _ = load(host, entry)
        assert assert_secret_template(host, inventory, entry, secret) != first_config, "Restart reused a prepared template"
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        for operation in ("down", "up"):
            changed = action(operation, request(project, profile, confirm=True, operation_id=str(uuid.uuid4())))
            assert changed["maintenance"]["down"] is (operation == "down")
            for unit in inventory.units.values():
                assert run(["systemctl", "is-active", unit]).stdout.strip() == "active"
            assert run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout == second
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        assert action("stop", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
        for unit in inventory.units.values():
            assert run(["systemctl", "is-active", "--quiet", unit], check=False).returncode != 0
        public_observation(host, topology, profile)
        print("PASS host capabilities: package, artifact, admission, private template, app binding, public TCP via nginx stream, report, restart, maintenance continuity and stop closure")


if __name__ == "__main__":
    main()
