#!/usr/bin/env python3
"""Opt-in generation-16 confinement, shared files, Unix sockets, templates and bindings in the disposable harness.

Use the ordinary deploy-acceptance archive. The artifact is a second, pinned copy
of that fixture's Linux artisan binary; no artifact network download is needed.
Lane B's archive verification and publication remain production code paths.
"""
from contextlib import ExitStack, closing
from dataclasses import asdict
import argparse
import copy
import hashlib
import io
import ipaddress
import json
import os
from pathlib import Path
import pwd
import secrets
import sys
import tarfile
import tomllib
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import artifacts
from contracts import DeployError, ReleaseDeclaration, digest, read_json
from host import Host
from topology import parse_topology, parse_profile
from acceptance_support import LAUNCHER, action, guard, request, run
import acceptance_host as shared
import acceptance_network as network
import edge
import firewall
import listeners
import network_confinement
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

PROCFS_PROBE = r'''
import errno, json, os, sys
from pathlib import Path

def assert_denied(probe):
    for path in probe['paths']:
        for mode in (os.O_RDONLY, os.O_WRONLY):
            try:
                fd = os.open(path, mode)
            except OSError as error:
                assert error.errno in (errno.EACCES, errno.EPERM), (path, mode, error.errno)
            else:
                os.close(fd)
                raise AssertionError(('procfs file access succeeded', path, mode))
    for path in probe['links']:
        try:
            os.readlink(path)
        except OSError as error:
            assert error.errno in (errno.EACCES, errno.EPERM), (path, error.errno)
        else:
            raise AssertionError(('procfs link traversal succeeded', path))

if __name__ == '__main__':
    uid, namespace, units, probe = sys.argv[1:]
    assert os.geteuid() == int(uid) > 0
    assert os.readlink('/proc/self/ns/user') == namespace
    status = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    assert int(status['CapEff'].strip(), 16) == 0
    group = Path('/proc/self/cgroup').read_text()
    assert not any(unit in group for unit in json.loads(units)), 'Probe must run outside workload units'
    assert_denied(json.loads(probe))
'''


def procfs_targets(pid, cache, cwd):
    prefix = f"/proc/{pid}"
    target = cache + "/frame.txt"
    return {"paths": [prefix + "/root" + target, prefix + "/cwd/" + os.path.relpath(target, cwd)],
            "links": [prefix + "/root", prefix + "/cwd", prefix + "/fd/1"]}


def assert_procfs_isolation(host, inventory, entry, cache):
    import fcntl
    import http.client
    import release
    import socket
    import struct
    observed = {name: release.service(host, unit) for name, unit in inventory.units.items()}
    namespaces, probes = {}, {}
    for role, state in observed.items():
        pid = int(state["MainPID"])
        assert pid > 0 and state["process"] and pid in listeners.process_members(host, inventory.units[role])
        namespace = f"/proc/{pid}/ns/user"
        namespaces[role] = os.readlink(namespace)
        assert namespaces[role] != os.readlink("/proc/self/ns/user")
        # NS_GET_OWNER_UID: systemd, not the instance UID, must own the namespace.
        with open(namespace, "rb") as file:
            assert struct.unpack("I", fcntl.ioctl(file.fileno(), 0xb704, struct.pack("I", 0)))[0] == 0
        status = dict(line.split(":", 1) for line in Path(f"/proc/{pid}/status").read_text().splitlines() if ":" in line)
        assert set(status["Uid"].split()) == {str(entry["allocations"]["uid"])}
        assert set(status["Gid"].split()) == {str(entry["allocations"]["gid"])}
        assert int(status["CapEff"].strip(), 16) == int(status["CapBnd"].strip(), 16) == 0
        if role not in ("http", "relay"):
            continue
        probe = procfs_targets(pid, cache, os.readlink(f"/proc/{pid}/cwd"))
        # Positive controls reject bogus paths and vanished targets. Preserve '..'
        # in the cwd route: it must resolve through the peer's procfs link first.
        for path in probe["paths"]:
            assert Path(path).read_bytes() == b"prepared-media"
            if role == "http":
                fd = os.open(path, os.O_WRONLY)
                os.close(fd)
        for path in probe["links"]:
            assert os.readlink(path)
        probes[role] = probe
    assert len(set(namespaces.values())) == len(namespaces), "Granted roles shared a user namespace"

    endpoint = next(e for e in inventory.resolution.endpoints if e.role == "relay")
    with closing(http.client.HTTPConnection(endpoint.address, endpoint.port, timeout=5)) as connection:
        connection.request("POST", "/shared-proc", body=json.dumps(probes["http"]),
                           headers={"Content-Type": "application/json"})
        response = connection.getresponse()
        assert response.status == 200 and response.read(128) == b"proc-denied", "Reader bypassed writer sandbox via procfs"
    host.run(["runuser", "-u", entry["name"], "--", "python3", "-I", "-c", PROCFS_PROBE,
              str(entry["allocations"]["uid"]), os.readlink("/proc/self/ns/user"),
              json.dumps(list(inventory.units.values())), json.dumps(probes["http"])], timeout=10)
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(inventory.resolution.unix_endpoints[0].path)
        connection.sendall(b"proc" + json.dumps(probes["relay"]).encode())
        assert connection.recv(32) == b"proc-denied", "Unlisted role bypassed reader sandbox via procfs"
    for name, before in observed.items():
        after = release.service(host, inventory.units[name])
        assert (before["MainPID"], before["process"]["start"]) == (after["MainPID"], after["process"]["start"])
    assert Path(cache, "frame.txt").read_bytes() == b"prepared-media"


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
    confinement, = [r for r in value["confinement"] if r["role"] == "wildcard"]
    assert confinement["state"] == "verified" and all(confinement["proof"]["programs"])
    private, = [r for r in value["private_listeners"] if r["role"] == "wildcard"]
    assert private["visibility"] == "private" and private["wildcard"] and private["confinement"] == "loopback"
    assert not any(r["port"] == private["port"] for r in value["unexpected_exposure"])
    assert private["port"] not in result["host"]["network"]["public_listeners"]
    assert not any(f["code"] in {"network_confinement_unverified", "listener_owner_unverified", "nginx_projection_unverified", "declared_listener_absent"}
                   for f in value["findings"])


def assert_public_listener_binding(inventory, profile):
    public, = inventory.specs["http"].bindings.public_listeners
    assert (public.name, public.role, public.listener, public.hostname, public.port, public.tls, public.transport,
            public.public_addresses) == ("relay_public", "relay", "control", "relay." + profile["domain"],
                                         PORT, "none", "tcp", tuple(network.ADDRESSES))


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


def assert_shared_files(host, inventory, entry, report):
    import http.client
    import backup
    binding, = inventory.specs["relay"].bindings.shared_directories
    assert binding.access == "read-only" and binding.max_bytes == 1048576
    assert inventory.specs["http"].bindings.shared_directories[0].access == "read-write"
    assert all(b.name != "render-cache" for b in inventory.specs["socket-provider"].bindings.shared_directories)
    assert Path(binding.path, "frame.txt").read_bytes() == b"prepared-media"
    import socket
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(inventory.resolution.unix_endpoints[0].path)
        connection.sendall(b"deny")
        assert connection.recv(32) == b"denied", "Unlisted running role could access the writer's existing file"
    endpoint = next(e for e in inventory.resolution.endpoints if e.role == "relay")
    connection = http.client.HTTPConnection(endpoint.address, endpoint.port, timeout=5)
    try:
        connection.request("GET", "/shared")
        response = connection.getresponse()
        assert response.status == 200 and response.read(128) == b"shared-read-only;writer-enospc"
    finally:
        connection.close()
    assert_procfs_isolation(host, inventory, entry, binding.path)
    # The evidence comes from the real reader process in its unit, not runuser
    # outside the sandbox (all roles deliberately share the same UID).
    info = Path(binding.path).lstat()
    import stat
    assert stat.S_IMODE(info.st_mode) == 0o700
    assert (info.st_uid, info.st_gid) == (entry["allocations"]["uid"], entry["allocations"]["gid"])
    capacity = __import__("os").statvfs(binding.path)
    assert capacity.f_blocks * capacity.f_frsize == binding.max_bytes
    coverage = backup.service_coverage(inventory)
    cache = next(item for item in coverage["shared_directories"] if item["name"] == "render-cache")
    assert cache["state"] == "disposable" and not cache["backed_up"] and not cache["restored"]
    assert "shared_directories" not in coverage["supported_components"]
    # Inspect the real encrypted-configuration input archive before encryption.
    files, _ = backup._archive_files(host, entry)
    assert not any("render-cache" in name or "/shared/" in name for name in files)
    assert not any(Path(binding.path) == path or Path(binding.path) in path.parents for path in files.values())
    instances = [instance for project in report["projects"] for instance in project["instances"]]
    instance = next(instance for instance in instances if instance["name"] == entry["name"])
    cache = next(item for item in instance["shared_directories"] if item["name"] == "render-cache")
    assert cache["status"] == "verified" and cache["bound"] == "tmpfs"
    assert cache["state"] == "disposable" and not cache["backed_up"] and not cache["restored"]


def assert_unix_binding(host, inventory, entry):
    import stat
    import time
    endpoint = inventory.specs["relay"].bindings.endpoints[0].endpoint
    info = Path(endpoint.directory).lstat()
    assert stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
    assert (info.st_uid, info.st_gid) == (entry["allocations"]["uid"], entry["allocations"]["gid"])
    assert stat.S_ISSOCK(Path(endpoint.path).lstat().st_mode)
    assert services.wait_ready(host, inventory, "socket-provider", time.monotonic() + 15)
    # Run as the consumer account using its delivered descriptor, not a guessed path.
    from runtime_config import write_binding_descriptor
    descriptor = write_binding_descriptor(host, entry, inventory.deployment, inventory.resolution, inventory.specs["relay"])
    probe = ("import json,socket,sys; d=json.load(open(sys.argv[1])); "
             "b=next(b for b in d['bindings']['endpoints'] if b['name']=='socket'); "
             "s=socket.socket(socket.AF_UNIX); s.settimeout(3); s.connect(b['endpoint']['path']); "
             "s.sendall(b'ping'); assert s.recv(32)==b'unix-ready'")
    host.run(["runuser", "-u", entry["name"], "--", "python3", "-I", "-c", probe, str(descriptor)])
    for role in ("relay", "socket-provider"):
        unit = run(["systemctl", "cat", inventory.units[role]]).stdout
        assert endpoint.directory in next(line for line in unit.splitlines() if line.startswith("ReadWritePaths="))
        assert "PrivateTmp=yes" in unit and "ProtectSystem=strict" in unit
    return endpoint


def feature_archive(source, destination, host_id, *, confined=False):
    with tarfile.open(source, "r:gz") as incoming:
        files = {item.name: incoming.extractfile(item).read() for item in incoming if item.isfile()}
    manifest = json.loads(files.pop("manifest.json"))
    project = {"schema": 1, "id": manifest["project_id"], "slug": manifest["instance"].removesuffix("-" + manifest["environment"])}
    profile = read_json(Path("/verification/foundry-go/tests/fixtures/deploy-acceptance/deploy/environments/acceptance.json"))
    profile.update(schema=3, architecture=manifest["architecture"], instance_id=manifest["instance_id"],
                   placement={"http": 1, "relay": 1, "socket-provider": 1}, listeners={})
    # Synthetic certificates are issued only for the disposable acceptance zone.
    profile["domain"] = f"{project['slug']}-{profile['environment']}.acceptance.invalid"
    profile["host"]["host_id"] = host_id
    profile["listeners"] = {"relay.control": {"ports": [PORT],
        "source_cidrs": [network.PEER_ADDRESSES[0] + "/32", network.PEER_ADDRESSES[1] + "/128"],
        "dns": {"mode": "external", "hostname": "relay." + profile["domain"],
                "public_addresses": list(network.ADDRESSES), "proxied": False}}}
    checksum = hashlib.sha256(files["artisan"]).hexdigest()
    platform = "linux-" + profile["architecture"]
    topology = parse_topology({"schema": 2, "shared_directories": {
        "render-cache": {"state": "disposable", "max_bytes": 1048576, "writers": ["http"], "readers": ["relay"]},
        # Exercise Unix readiness and stale cleanup for a namespaced provider,
        # which remains unlisted for the media cache.
        "probe-cache": {"state": "disposable", "max_bytes": 65536, "writers": ["socket-provider"], "readers": []}}, "roles": [
        {"name": "http", "kind": "builtin", "requires": ["relay"], "packages": ["jq"],
         "bindings": {"relay": {"endpoint": {"role": "relay", "listener": "control", "scheme": "http"}},
                      "relay_public": {"public_listener": {"role": "relay", "listener": "control"}},
                      "checksum": {"executable": {"package": "jq", "path": "/usr/bin/jq"}}}},
        {"name": "relay", "kind": "executable", "executable": {"artifact": "relay", "entry": "server"},
         "arguments": [{"literal": "feature:relay"}, {"listener_address": "control"}, {"listener_port": "control"},
                       {"binding_socket_path": "socket"}, {"shared_directory_path": "render-cache"}],
         "requires": ["socket-provider"],
         "bindings": {"socket": {"endpoint": {"role": "socket-provider", "listener": "control", "scheme": "unix"}}},
         "environment": {"XDG_RUNTIME_DIR": {"binding_socket_directory": "socket"},
                         "WAYLAND_DISPLAY": {"binding_socket_name": "socket"},
                         "RENDER_CACHE": {"shared_directory_path": "render-cache"}},
         "secret_files": {"TOKEN_FILE": "relay-template-token"},
         "configs": {"private": {"source": "deploy/files/relay.yaml", "mode": "rendered-template",
                                 "format": "yaml", "path": "relay.yaml", "directory_environment": "XDG_CONFIG_HOME"}},
         "listeners": {"control": {"transport": "tcp", "bind": "public", "tls": "none", "authentication": True}},
         "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200, "body": "feature-relay-ready"},
         "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
         "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
         "maintenance": "continue", "state": {"kind": "disposable"}},
        {"name": "socket-provider", "kind": "executable",
         "executable": {"artifact": "relay", "entry": "server"},
         "arguments": [{"literal": "feature:unix"}, {"listener_socket_path": "control"}],
         "environment": {"XDG_RUNTIME_DIR": {"listener_socket_directory": "control"}},
         "listeners": {"control": {"transport": "unix", "socket": "fixture.sock"}},
         "readiness": {"kind": "unix", "listener": "control", "timeout_seconds": 15},
         "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
         "limits": {"memory_bytes": 67108864, "cpu_percent": 100, "tasks": 16},
         "maintenance": "continue", "state": {"kind": "disposable"}}],
        "secrets": {"relay-template-token": {"prompt": "Relay template token"}},
        "artifacts": {"relay": {"version": "fixture-1", "format": "file", "executables": {"server": "relay"},
            "platforms": {platform: {"url": "https://acceptance.invalid/relay", "sha256": checksum}}}}})
    if confined:
        python = f"python{sys.version_info.major}.{sys.version_info.minor}"
        profile["placement"]["wildcard"] = 1
        raw = topology.projection()
        raw["roles"][0]["requires"].append("wildcard")
        raw["roles"].append({
            "name": "wildcard", "kind": "executable", "packages": [python, python + "-minimal"],
            "executable": {"package": python + "-minimal", "path": "/usr/bin/" + python},
            "network": {"confinement": "loopback"},
            "arguments": [{"literal": "-I"}, {"literal": "-c"}, {"literal":
                "import socket,sys; s=socket.create_server(('::',int(sys.argv[1])),family=socket.AF_INET6,dualstack_ipv6=True); "
                "exec('while True:\\n c,a=s.accept(); c.close()')"}, {"listener_port": "control"}],
            "listeners": {"control": {"transport": "tcp", "bind": "loopback", "wildcard": True}},
            "readiness": {"kind": "tcp", "listener": "control", "timeout_seconds": 15},
            "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
            "limits": {"memory_bytes": 67108864, "cpu_percent": 100, "tasks": 16},
            "maintenance": "continue", "state": {"kind": "disposable"}})
        topology = parse_topology(raw)
        invalid = copy.deepcopy(raw)
        invalid["roles"][-1].pop("network")
        try:
            parse_topology(invalid)
        except DeployError as error:
            assert error.code == "invalid_topology" and "confinement" in error.detail
        else:
            raise AssertionError("Unconfined wildcard declaration was accepted")
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


def assert_confined_wildcard(host, inventory):
    import time
    endpoint = next(e for e in inventory.resolution.endpoints if e.role == "wildcard")
    assert services.wait_ready(host, inventory, "wildcard", time.monotonic() + 15)
    proof = network_confinement.isolation(host, inventory, "wildcard")
    assert len(proof["programs"]) == 2 and all(proof["programs"])
    owners = listeners.process_members(host, inventory.units["wildcard"])
    sockets = [s for s in listeners.observe_sockets(host) if s.socket.port == endpoint.port]
    assert sockets and all(ipaddress.ip_address(s.socket.address).is_unspecified and s.pids
                           and set(s.pids) <= owners for s in sockets)
    # Explicit accepts remove default-deny firewall as an explanation for isolation.
    with ExitStack() as cleanup:
        for tool in ("iptables", "ip6tables"):
            for chain in ("INPUT", "OUTPUT"):
                for direction in ("--dport", "--sport"):
                    rule = [chain, "-p", "tcp", direction, str(endpoint.port), "-j", "ACCEPT"]
                    host.run([tool, "-I", *rule])
                    cleanup.callback(host.run, [tool, "-D", *rule])
        for prefix in ([], ["ip", "netns", "exec", network.PEER]):
            for address in network.ADDRESSES:
                result = host.run([*prefix, "python3", "-I", "-c", PEER_PROBE, address, str(endpoint.port), "no"],
                                  check=False, timeout=8)
                assert result.returncode == 2 and not result.stdout, "Confined port reachable on a non-loopback address"
    assert not any(c.socket.port == endpoint.port for c in listeners.public_claims(inventory.profile))
    assert not any(r.port == endpoint.port for r in edge.upstream_references(host))
    return endpoint


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    args = parser.parse_args()
    guard()
    host = Host()
    assert host.read_registry(missing=True) is None, "Feature scenario requires a fresh disposable host"
    shared.WORK.mkdir(mode=0o700, exist_ok=True)
    archive = shared.WORK / "feature.tar.gz"
    project, profile, topology, disks = feature_archive(args.archive, archive, shared.HOST_ID, confined=True)
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
        assert_public_listener_binding(inventory, profile)
        assert_confined_wildcard(host, inventory)
        for unit in inventory.units.values():
            assert run(["systemctl", "is-active", unit]).stdout.strip() == "active"
        endpoint = assert_unix_binding(host, inventory, entry)
        # Hold the old inode open across restart so its replacement cannot reuse it.
        import os
        old_socket = os.open(endpoint.path, os.O_PATH | os.O_NOFOLLOW)
        cleanup.callback(os.close, old_socket)
        old_inode = os.fstat(old_socket).st_ino
        first_config = assert_secret_template(host, inventory, entry, secret)
        # The fixture loads both private and public bindings before HTTP readiness.
        import time
        assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        reported = json.loads(run([LAUNCHER, "report"], input=json.dumps(request(project, profile))).stdout)
        assert_listener_report(reported, entry)
        assert_shared_files(host, inventory, entry, reported)
        first = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
        assert action("restart", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
        second = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
        assert first.strip() and second.strip() and first != second
        assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
        _, entry = host.require_identity(project, profile)
        _, _, inventory, _ = load(host, entry)
        assert_public_listener_binding(inventory, profile)
        assert_confined_wildcard(host, inventory)
        assert assert_secret_template(host, inventory, entry, secret) != first_config, "Restart reused a prepared template"
        assert_unix_binding(host, inventory, entry)
        reported = json.loads(run([LAUNCHER, "report"], input=json.dumps(request(project, profile))).stdout)
        assert_shared_files(host, inventory, entry, reported)
        assert Path(endpoint.path).stat().st_ino != old_inode, "Restart did not replace the stale provider socket"
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
        # Use the same cleanup owner as release transitions after confirmed stop.
        import shared_directories
        from feature_lifecycle import stop_units
        stop_units(host, inventory)
        shared_directories.discard(host, inventory.topology, profile["instance_id"],
                                   entry["allocations"]["uid"], entry["allocations"]["gid"])
        assert not Path(inventory.specs["http"].bindings.shared_directories[0].path).exists()
        print("PASS host capabilities: package, artifact, admission, templates, private/public/Unix app bindings, verified wildcard confinement, shared files with reader EROFS and writer ENOSPC, disposable backup exclusion, stale-socket restart, public TCP via nginx stream, report, restart, maintenance continuity and stop closure")


if __name__ == "__main__":
    main()
