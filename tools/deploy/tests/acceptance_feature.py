#!/usr/bin/env python3
"""Opt-in generation-19 numbered copies and host capabilities in the disposable harness.

Use the ordinary deploy-acceptance archive. The artifact is a second, pinned copy
of that fixture's Linux artisan binary; no artifact network download is needed.
Lane B's archive verification and publication remain production code paths.
"""
from contextlib import ExitStack, closing, contextmanager
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
import select
import signal
import subprocess
import sys
import tarfile
import tomllib
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import unit_selection_names
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
import acceptance_copies_consumers
import acceptance_copies_ops

PORT = 8554
RELAY_BODY = "feature-relay-ready"
COPIES_ROLE = "copies"
COPIES_PROGRAM = '''import errno,http.server,json,os,socket,sys,tempfile,threading
from pathlib import Path
port,unix_path,cache,state,provider,ordinal=sys.argv[1:]
ordinal=int(ordinal)
state=Path(state)
state.mkdir(parents=True,exist_ok=True)
marker=state/'identity.json'
if not marker.exists():
 marker.write_text(json.dumps({'ordinal':ordinal,'created_pid':os.getpid()}))
assert json.loads(marker.read_text())['ordinal']==ordinal
unix=socket.socket(socket.AF_UNIX)
unix.bind(unix_path)
unix.listen()
def serve_unix():
 while True:
  connection,_=unix.accept()
  with connection:
   try: connection.sendall(str(ordinal).encode())
   except BrokenPipeError: pass
threading.Thread(target=serve_unix,daemon=True).start()
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path not in ('/ready','/frame'):
   self.send_error(404)
   return
  frame_ready=None
  if self.path=='/frame':
   shared=Path(cache)/'frame.txt'
   frame_ready=shared.exists() and shared.read_bytes()==b'prepared-media'
  try:
   if self.path=='/ready':
    descriptor,probe=tempfile.mkstemp(prefix='.readiness-',dir=cache)
   else:
    descriptor=os.open(shared,os.O_WRONLY)
  except OSError as error:
   read_only=error.errno==errno.EROFS
  else:
   os.close(descriptor)
   if self.path=='/ready': os.unlink(probe)
   read_only=False
  with socket.socket(socket.AF_UNIX) as connection:
   connection.settimeout(3)
   connection.connect(provider)
   connection.sendall(b'ping')
   provider_ready=connection.recv(32)==b'unix-ready'
  result={'ordinal':ordinal,'pid':os.getpid(),'state':str(state),'unix':unix_path,'shared_read_only':read_only,'provider_ready':provider_ready}
  if frame_ready is not None: result['frame_ready']=frame_ready
  body=json.dumps(result).encode()
  self.send_response(200 if read_only and provider_ready and frame_ready is not False else 503)
  self.end_headers()
  self.wfile.write(body)
 def log_message(self,*args): pass
class Server(http.server.HTTPServer):
 address_family=socket.AF_INET6
 def server_bind(self):
  self.socket.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,0)
  super().server_bind()
Server(('::',int(port)),Handler).serve_forever()
'''
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

def assert_host_denied(cache):
    for mode in (os.O_RDONLY, os.O_WRONLY):
        try:
            fd = os.open(cache + '/frame.txt', mode)
        except OSError as error:
            assert error.errno == errno.EACCES, (mode, error.errno)
        else:
            os.close(fd)
            raise AssertionError(('host cache access succeeded', mode))

if __name__ == '__main__':
    uid, namespace, mount_namespace, units, probe, cache = sys.argv[1:]
    assert os.geteuid() == int(uid) > 0
    assert os.readlink('/proc/self/ns/user') == namespace
    assert os.readlink('/proc/self/ns/mnt') == mount_namespace
    status = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
    assert int(status['CapEff'].strip(), 16) == 0
    group = Path('/proc/self/cgroup').read_text()
    assert not any(unit in group for unit in json.loads(units)), 'Probe must run outside workload units'
    assert_denied(json.loads(probe))
    assert_host_denied(cache)
    print(os.getpid(), flush=True)
    assert sys.stdin.readline() == 'stop\n'
'''


@contextmanager
def host_account_helper(inventory, entry, cache, probe):
    """Keep the tested same-account host process alive until the unit probes finish."""
    process = subprocess.Popen(["runuser", "-u", entry["name"], "--", "python3", "-I", "-c", PROCFS_PROBE,
        str(entry["allocations"]["uid"]), os.readlink("/proc/self/ns/user"), os.readlink("/proc/self/ns/mnt"),
        json.dumps(list(inventory.units.values())), json.dumps(probe), cache],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, start_new_session=True)
    try:
        assert select.select([process.stdout], [], [], 10)[0], "Host helper did not become ready"
        ready = os.read(process.stdout.fileno(), 64)
        assert ready.endswith(b"\n") and ready.strip().isdigit(), "Host helper failed its isolation checks"
        pid = int(ready)
        assert pid > 0
        yield pid
        assert process.poll() is None, "Host helper exited during the probes"
    finally:
        try:
            process.communicate(b"stop\n", timeout=5)
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate(timeout=5)
            raise
    assert process.returncode == 0, "Host helper failed to exit cleanly"


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
        if inventory.specs[role].bindings.shared_directories:
            assert namespaces[role] != os.readlink("/proc/self/ns/user")
            # NS_GET_OWNER_UID: systemd, not the instance UID, must own the namespace.
            with open(namespace, "rb") as file:
                assert struct.unpack("I", fcntl.ioctl(file.fileno(), 0xb704, struct.pack("I", 0)))[0] == 0
        else:
            assert namespaces[role] == os.readlink("/proc/self/ns/user"), "Ungranted role must retain the host user namespace"
        status = dict(line.split(":", 1) for line in Path(f"/proc/{pid}/status").read_text().splitlines() if ":" in line)
        assert set(status["Uid"].split()) == {str(entry["allocations"]["uid"])}
        assert set(status["Gid"].split()) == {str(entry["allocations"]["gid"])}
        assert int(status["CapEff"].strip(), 16) == int(status["CapBnd"].strip(), 16) == 0
        if role.role not in ("http", "relay"):
            continue
        probe = procfs_targets(pid, cache, os.readlink(f"/proc/{pid}/cwd"))
        # Positive controls reject bogus paths and vanished targets. Preserve '..'
        # in the cwd route: it must resolve through the peer's procfs link first.
        for path in probe["paths"]:
            assert Path(path).read_bytes() == b"prepared-media"
            if role.role == "http":
                fd = os.open(path, os.O_WRONLY)
                os.close(fd)
        for path in probe["links"]:
            assert os.readlink(path)
        probes[role] = probe
    granted = [namespaces[role] for role in namespaces if inventory.specs[role].bindings.shared_directories]
    assert len(set(granted)) == len(granted), "Granted roles shared a user namespace"

    endpoint = next(e for e in inventory.resolution.endpoints if e.role == "relay")
    with closing(http.client.HTTPConnection(endpoint.address, endpoint.port, timeout=5)) as connection:
        connection.request("POST", "/shared-proc", body=json.dumps(probes[inventory.identity("http")]),
                           headers={"Content-Type": "application/json"})
        response = connection.getresponse()
        assert response.status == 200 and response.read(128) == b"proc-denied", "Reader bypassed writer sandbox via procfs"
    with host_account_helper(inventory, entry, cache, probes[inventory.identity("http")]) as pid:
        host_path = f"/proc/{pid}/root{cache}/frame.txt"
        assert Path(host_path).read_bytes() == b"prepared-media"
        fd = os.open(host_path, os.O_WRONLY)
        os.close(fd)
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(5)
            connection.connect(inventory.resolution.unix_endpoints[0].path)
            connection.sendall(b"host" + json.dumps(pid).encode() + b"\n")
            assert connection.recv(32) == b"host-denied", "Ungranted role bypassed the host parent via a helper"
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(inventory.resolution.unix_endpoints[0].path)
        connection.sendall(b"proc" + json.dumps(probes[inventory.identity("relay")]).encode())
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
    public, = inventory.specs[inventory.identity("http")].bindings.public_listeners
    assert (public.name, public.role, public.listener, public.hostname, public.port, public.tls, public.transport,
            public.public_addresses) == ("relay_public", "relay", "control", "relay." + profile["domain"],
                                         PORT, "none", "tcp", tuple(network.ADDRESSES))


def assert_secret_template(host, inventory, entry, secret):
    paths = services.ServicePaths.for_unit(inventory.specs[inventory.identity("relay")].identity, inventory.resolution.id)
    config = Path(paths.config("private", "rendered-template", "relay.yaml"))
    account = pwd.getpwnam(entry["name"])
    assert config.is_file(), "Prepared secret template is missing"
    assert (config.stat().st_uid, config.stat().st_gid, config.stat().st_mode & 0o777) == (0, account.pw_gid, 0o640)
    assert json.loads(config.read_text().removeprefix("token: ")) == secret, "Prepared template differs from private input"
    baseline = Path(paths.base) / "current/baselines/private"
    assert baseline.read_bytes() == config.read_bytes()
    assert (baseline.stat().st_uid, baseline.stat().st_gid, baseline.stat().st_mode & 0o777) == (0, account.pw_gid, 0o640)
    assert run(["runuser", "-u", entry["name"], "--", "test", "-r", str(config)], check=False).returncode == 0
    home = Path(paths.base) / "current/config/fixture-home"
    tree = home / ".config/fixture"
    assert services.config_environment(inventory.roles["relay"], paths)["HOME"] == str(home)
    assert json.loads((tree / "global.json").read_text()) == {"token": secret}
    assert (tree / "profiles/stream/basic.ini").read_text() == "[Output]\nMode=Advanced\n"
    assert json.loads((tree / "profiles/stream/streamEncoder.json").read_text()) == {"keyint_sec": 1}
    for path in tree.rglob("*"):
        assert not path.is_symlink()
        assert path.stat().st_uid == account.pw_uid
        assert path.stat().st_mode & 0o777 == (0o700 if path.is_dir() else 0o600)
    assert run(["runuser", "-u", entry["name"], "--", "test", "-w", str(tree / "global.json")], check=False).returncode == 0
    relay_unit = inventory.units[inventory.identity("relay")]
    assert "HOME=" + str(home) in run(["systemctl", "cat", relay_unit]).stdout
    for pid in listeners.process_members(host, relay_unit):
        assert ("HOME=" + str(home)).encode() in Path(f"/proc/{pid}/environ").read_bytes().split(b"\0")
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
    binding, = inventory.specs[inventory.identity("relay")].bindings.shared_directories
    assert binding.access == "read-only" and binding.max_bytes == 1048576
    assert inventory.specs[inventory.identity("http")].bindings.shared_directories[0].access == "read-write"
    assert not inventory.specs[inventory.identity("socket-provider")].bindings.shared_directories
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
    parent = Path(binding.path).parent.lstat()
    assert stat.S_ISDIR(parent.st_mode) and stat.S_IMODE(parent.st_mode) == 0o700
    assert (parent.st_uid, parent.st_gid) == (0, 0)
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
    endpoint = inventory.specs[inventory.identity("relay")].bindings.endpoints[0].endpoint
    info = Path(endpoint.directory).lstat()
    assert stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
    assert (info.st_uid, info.st_gid) == (entry["allocations"]["uid"], entry["allocations"]["gid"])
    assert stat.S_ISSOCK(Path(endpoint.path).lstat().st_mode)
    assert services.wait_ready(host, inventory, inventory.identity("socket-provider"), time.monotonic() + 15)
    # Run as the consumer account using its delivered descriptor, not a guessed path.
    from runtime_config import write_binding_descriptor
    descriptor = write_binding_descriptor(host, entry, inventory.deployment, inventory.resolution, inventory.specs[inventory.identity("relay")])
    probe = ("import json,socket,sys; d=json.load(open(sys.argv[1])); "
             "b=next(b for b in d['bindings']['endpoints'] if b['name']=='socket'); "
             "s=socket.socket(socket.AF_UNIX); s.settimeout(3); s.connect(b['endpoint']['path']); "
             "s.sendall(b'ping'); assert s.recv(32)==b'unix-ready'")
    host.run(["runuser", "-u", entry["name"], "--", "python3", "-I", "-c", probe, str(descriptor)])
    for role in inventory.select(roles=["relay", "socket-provider"]):
        unit = run(["systemctl", "cat", inventory.units[role]]).stdout
        assert endpoint.directory in next(line for line in unit.splitlines() if line.startswith("ReadWritePaths="))
        assert "PrivateTmp=yes" in unit and "ProtectSystem=strict" in unit
    return endpoint


def feature_archive(source, destination, host_id, *, confined=False, copies=0, version=None):
    with tarfile.open(source, "r:gz") as incoming:
        files = {item.name: incoming.extractfile(item).read() for item in incoming if item.isfile()}
    manifest = json.loads(files.pop("manifest.json"))
    if version is not None:
        prefix = "private-source-maps/" + manifest["version"] + "/"
        files = {"private-source-maps/" + version + "/" + name[len(prefix):] if name.startswith(prefix) else name: data
                 for name, data in files.items()}
        manifest["version"] = version
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
    # Containers mount the framework at /verification/foundry-go; unit tests run from the repository.
    fixture = next(root for root in (Path("/verification/foundry-go/tests/fixtures/deploy-acceptance"),
                                     Path(__file__).resolve().parents[3] / "tests/fixtures/deploy-acceptance")
                   if (root / "deploy/rehearsal/topology.json").is_file())
    tree_configs = json.loads((fixture / "deploy/rehearsal/topology.json").read_text())["roles"][1]["configs"]
    topology = parse_topology({"schema": 2, "shared_directories": {
        "render-cache": {"state": "disposable", "max_bytes": 1048576, "writers": ["http"], "readers": ["relay"]}}, "roles": [
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
         "configs": {**tree_configs, "private": {"source": "deploy/files/relay.yaml", "mode": "rendered-template",
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
    if copies:
        python = f"python{sys.version_info.major}.{sys.version_info.minor}"
        profile["placement"][COPIES_ROLE] = copies
        raw = topology.projection()
        raw["shared_directories"]["render-cache"]["readers"].append(COPIES_ROLE)
        raw["secrets"]["copy-token"] = {"scope": "copy", "prompt": "Copy token"}
        raw["roles"][0]["bindings"]["copies"] = {"copies": {"role": COPIES_ROLE,
            "listener": "control", "scheme": "http", "secret": "copy-token"}}
        raw["roles"].append({
            "name": COPIES_ROLE, "kind": "executable",
            "secret_files": {"COPY_TOKEN_FILE": {"secret": "copy-token", "scope": "copy"}}, "packages": [python, python + "-minimal"],
            "executable": {"package": python + "-minimal", "path": "/usr/bin/" + python},
            "network": {"confinement": "loopback"},
            "requires": ["socket-provider"],
            "bindings": {"socket": {"endpoint": {"role": "socket-provider", "listener": "control", "scheme": "unix"}}},
            "arguments": [{"literal": "-I"}, {"literal": "-c"}, {"literal": "exec(" + repr(COPIES_PROGRAM) + ")"},
                          {"listener_port": "control"}, {"listener_socket_path": "control-unix"},
                          {"shared_directory_path": "render-cache"}, {"state_path": "probe"},
                          {"binding_socket_path": "socket"}, {"copy_number": "self"}],
            "listeners": {"control": {"transport": "tcp", "bind": "loopback", "wildcard": True},
                          "control-unix": {"transport": "unix", "socket": "copies.sock"}},
            "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200, "timeout_seconds": 15},
            "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
            "limits": {"memory_bytes": 67108864, "cpu_percent": 100, "tasks": 16},
            "maintenance": "stop", "state": {"kind": "local-unprotected", "acknowledged": True}})
        topology = parse_topology(raw)
    profile = parse_profile(profile, topology).projection()
    payload_path = "deploy/artifacts/relay/" + checksum + ".bin"
    files[payload_path] = files["artisan"]
    files["deploy/files/relay.yaml"] = b"token: {{secret.TOKEN_FILE}}\n"
    for config in tree_configs.values():
        for file in config["files"].values():
            files[file["source"]] = (fixture / file["source"]).read_bytes()
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
    assert services.wait_ready(host, inventory, inventory.identity("wildcard"), time.monotonic() + 15)
    proof = network_confinement.isolation(host, inventory, inventory.identity("wildcard"))
    assert len(proof["programs"]) == 2 and all(proof["programs"])
    owners = listeners.process_members(host, inventory.units[inventory.identity("wildcard")])
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


def assert_numbered_copies(host, inventory, entry, count):
    """Inspect each real copy after activation has started HTTP and written its frame."""
    import http.client
    import socket
    import time
    import release
    import unix_sockets
    from contracts import copy_socket_name
    selected = inventory.copies(COPIES_ROLE)
    assert tuple(identity.ordinal for identity in selected) == tuple(range(1, count + 1))
    assert services.wait_ready_set(host, inventory, selected, time.monotonic() + 30)
    claims = listeners.private_claims(inventory.profile.instance_id, inventory.resolution, inventory.topology)
    observed = listeners.observe_sockets(host)
    snapshot = {}
    for identity in selected:
        unit, spec = inventory.units[identity], inventory.specs[identity]
        tcp, = [e for e in inventory.resolution.endpoints if (e.role, e.ordinal) == (identity.role, identity.ordinal)]
        unix, = [e for e in inventory.resolution.unix_endpoints if (e.role, e.ordinal) == (identity.role, identity.ordinal)]
        assert unix.name == copy_socket_name("copies.sock", identity.ordinal)
        assert unit.endswith(f"-{COPIES_ROLE}@{identity.ordinal}.service")
        assert spec.arguments[-1] == str(identity.ordinal)
        live = release.service(host, unit)
        assert live["ActiveState"] == "active" and live["NRestarts"] == "0" and live["process"]
        members = listeners.process_members(host, unit)
        sockets = [row for row in observed if row.socket.port == tcp.port]
        assert sockets and all(ipaddress.ip_address(row.socket.address).is_unspecified and row.pids
                               and set(row.pids) <= members for row in sockets)
        claim, = [claim for claim in claims if claim.identity == identity]
        assert claim.wildcard and not listeners.ownership_findings((claim,), sockets, {identity: members})
        proof = network_confinement.isolation(host, inventory, identity)
        assert len(proof["programs"]) == 2 and all(proof["programs"])
        assert unix_sockets.ready(host, unix, entry["allocations"]["uid"], entry["allocations"]["gid"],
                                  unit, time.monotonic() + 5)
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(3)
            connection.connect(unix.path)
            assert connection.recv(32) == str(identity.ordinal).encode()
        with closing(http.client.HTTPConnection(tcp.address, tcp.port, timeout=3)) as connection:
            connection.request("GET", "/frame")
            response = connection.getresponse()
            assert response.status == 200
            delivered = json.loads(response.read(4096))
        paths = services.ServicePaths.for_unit(identity, inventory.resolution.id)
        state = Path(paths.state) / "data/probe"
        assert delivered == {"ordinal": identity.ordinal, "pid": int(live["MainPID"]), "state": str(state),
                             "unix": unix.path, "shared_read_only": True, "provider_ready": True, "frame_ready": True}
        provider, = spec.bindings.endpoints
        assert provider.name == "socket" and provider.endpoint.role == "socket-provider"
        grant, = spec.bindings.shared_directories
        assert grant.name == "render-cache" and grant.access == "read-only"
        marker = state / "identity.json"
        assert json.loads(marker.read_text())["ordinal"] == identity.ordinal
        snapshot[identity] = {"unit": unit, "pid": live["MainPID"], "start": live["process"]["start"],
                              "port": tcp.port, "socket": unix.path, "socket_inode": Path(unix.path).stat().st_ino,
                              "state": str(state), "marker": marker.read_bytes()}
    assert len({item["port"] for item in snapshot.values()}) == count
    assert len({item["socket"] for item in snapshot.values()}) == count
    assert len({item["state"] for item in snapshot.values()}) == count
    assert not inventory.specs[inventory.identity("socket-provider")].bindings.shared_directories
    return snapshot


def check_copy_consumers(host, entry, inventory, *, previous_paths=None):
    return acceptance_copies_consumers.check(host, entry, inventory,
        consumer=inventory.identity("http"), binding_name="copies", producer=COPIES_ROLE,
        secret_variable="COPY_TOKEN_FILE", previous_paths=previous_paths, check_console=True)


def numbered_count_changes(host, project, profile, inventory, entry, source):
    import admission
    from copy_enablement import removed_enablement
    from contracts import ClosureEvidence, ClosureStage
    from feature_lifecycle import load
    original_profile = copy.deepcopy(profile)
    original_count = profile["placement"][COPIES_ROLE]
    previous = assert_numbered_copies(host, inventory, entry, original_count)
    original_snapshot = previous
    private_paths = check_copy_consumers(host, entry, inventory)
    original_secrets = {name: Path(path).read_bytes() for name, path in private_paths.items()}
    rollback_target = None
    restored_snapshot = None

    def resize(count):
        nonlocal profile, inventory, entry, previous, private_paths
        archive = shared.WORK / f"feature-copies-{count}.tar.gz"
        stamp = inventory.deployment.release.split("-", 1)[0]
        version = stamp + "-" + hashlib.sha256((inventory.deployment.release + str(count) + uuid.uuid4().hex).encode()).hexdigest()[:40]
        _, target, _, _ = feature_archive(source, archive, profile["host"]["host_id"],
                                          confined=True, copies=count, version=version)
        activation = request(project, profile, archive=str(archive), archive_sha256=digest(archive), operation_id=str(uuid.uuid4()))
        before_units = set(inventory.copies(COPIES_ROLE))
        from contracts import UnitIdentity
        after_units = {UnitIdentity(profile["instance_id"], COPIES_ROLE, ordinal) for ordinal in range(1, count + 1)}
        added, removed = after_units - before_units, before_units - after_units
        plan = action("release-plan", activation)
        assert set(plan["added_units"]) == set(unit_selection_names(sorted(added)))
        assert set(plan["removed_units"]) == set(unit_selection_names(sorted(removed)))
        assert set(unit_selection_names(sorted(before_units & after_units))) <= set(plan["retained_units"])
        result = action("activate", {**activation, "plan_token": plan["plan_token"]})
        assert result["ok"] and result["profile_update"] == target
        profile = target
        _, entry = host.require_identity(project, profile)
        _, _, inventory, _ = load(host, entry)
        current = assert_numbered_copies(host, inventory, entry, count)
        for identity in before_units & after_units:
            assert current[identity] == previous[identity], "Count change disturbed a surviving copy"
        if removed:
            store = admission.AdmissionStore(host, profile["instance_id"])
            closure = admission.read_owned(host, store.runtime / "boot.json", ClosureEvidence.parse)
            assert closure.stage is ClosureStage.APPLICATION_CLOSED and removed <= set(closure.units)
            assert closure.backend_exit_sha256 and closure.old_reference_isolation_sha256
        for identity in removed:
            assert removed_enablement(host, entry)[identity] is True
            old = previous[identity]
            services.stopped_unit(host, old["unit"])
            assert not Path("/etc/systemd/system", old["unit"]).exists(), "Removed copy unit was retained"
            runtime = host.paths.p("/run/foundry/services/" + identity.instance_id)
            name = identity.record_key
            assert not any((resolution / name).exists() for resolution in runtime.iterdir() if resolution.is_dir()), \
                "Removed copy runtime view was retained"
            assert Path(old["state"], "identity.json").read_bytes() == old["marker"], "Shrink deleted persistent copy data"
        private_paths = check_copy_consumers(host, entry, inventory, previous_paths=private_paths)
        previous = current

    for count in (4, 2):
        if count == 2:
            rollback_target = inventory.deployment.release
            restored_snapshot = previous
        resize(count)
    import backup
    files, metadata = backup._archive_files(host, entry)
    import runtime_config
    prefix = "instances/" + entry["name"] + "/"
    configuration = host.paths.instance_config(entry["name"])
    assert all(prefix + Path(path).relative_to(configuration).as_posix() in files
               for path in runtime_config.service_secret_snapshot_paths(host, entry))
    receipt = action("backup-run", request(project, profile, kind="full"), timeout=1800)
    assert receipt["status"] == "complete"
    acceptance_copies_ops.assert_copies_operations(host, project, profile, role=COPIES_ROLE, backup_id=receipt["backup_id"])
    rollback = request(project, profile, target=rollback_target, expected_current=inventory.deployment.release,
                       operation_id=str(uuid.uuid4()))
    reviewed = action("release-plan", rollback)
    restored = reviewed["restored_copies"]
    assert {(item["role"], item["ordinal"], item["enabled"], item["evidence"]) for item in restored} == {
        (COPIES_ROLE, 3, True, "recorded"), (COPIES_ROLE, 4, True, "recorded")}
    result = action("rollback", {**rollback, "plan_token": reviewed["plan_token"]})
    assert result["ok"]
    profile = result["profile_update"]
    _, entry = host.require_identity(project, profile)
    _, _, inventory, _ = load(host, entry)
    current = assert_numbered_copies(host, inventory, entry, 4)
    assert set(inventory.copies(COPIES_ROLE)) <= set(services.enabled_units(host, inventory.units))
    for identity, prior in restored_snapshot.items():
        assert current[identity]["state"] == prior["state"] and current[identity]["marker"] == prior["marker"]
    private_paths = check_copy_consumers(host, entry, inventory, previous_paths=private_paths)
    previous = current
    # Keep the rollback proof above, then exercise regrowth before handing the
    # original archive profile to the following recovery phase.
    resize(2)
    retained_enablement = removed_enablement(host, entry)
    resize(original_count)
    assert profile == original_profile, "Copies scenario did not restore the original archive profile"
    assert set(inventory.copies(COPIES_ROLE)) <= set(services.enabled_units(host, inventory.units))
    assert removed_enablement(host, entry) == retained_enablement, "Regrowth consumed removed-copy enablement history"
    for identity, prior in original_snapshot.items():
        assert previous[identity]["state"] == prior["state"] and previous[identity]["marker"] == prior["marker"], \
            "Regrowth replaced retained copy state"
    assert all(Path(private_paths[name]).read_bytes() == value for name, value in original_secrets.items()), \
        "Regrowth replaced a retained copy secret"
    return profile, inventory, entry


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    args = parser.parse_args()
    guard()
    import acceptance_dns
    acceptance_dns.run()
    host = Host()
    assert host.read_registry(missing=True) is None, "Feature scenario requires a fresh disposable host"
    shared.WORK.mkdir(mode=0o700, exist_ok=True)
    archive = shared.WORK / "feature.tar.gz"
    project, profile, topology, disks = feature_archive(args.archive, archive, shared.HOST_ID, confined=True, copies=3)
    shared.private_json(shared.WORK / "topology.json", {**topology.projection(), "sha256": topology.sha256})
    private = shared.bootstrap_secrets(disks)
    secret = secrets.token_urlsafe(32) + '"#;]雪'
    private.setdefault("configuration", {})["FOUNDRY_SERVICE_SECRET__RELAY_TEMPLATE_TOKEN"] = secret
    import runtime_config
    for key in runtime_config.service_secret_keys(topology):
        if key.startswith("FOUNDRY_SERVICE_SECRET__COPY__") and int(key.split("__")[-2]) <= 4:
            private["configuration"][key] = secrets.token_urlsafe(32)
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
        assert services.wait_ready(host, inventory, inventory.identity("http"), time.monotonic() + 90)
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        reported = json.loads(run([LAUNCHER, "report"], input=json.dumps(request(project, profile))).stdout)
        assert_listener_report(reported, entry)
        assert_shared_files(host, inventory, entry, reported)
        first = run(["systemctl", "show", inventory.units[inventory.identity("relay")], "-p", "InvocationID", "--value"]).stdout
        assert action("restart", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
        second = run(["systemctl", "show", inventory.units[inventory.identity("relay")], "-p", "InvocationID", "--value"]).stdout
        assert first.strip() and second.strip() and first != second
        assert services.wait_ready(host, inventory, inventory.identity("http"), time.monotonic() + 90)
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
        # Recreate the cache state seen after reboot, then use the real boot owners.
        import feature_lifecycle
        import feature_network
        import shared_directories
        feature_lifecycle.stop_units(host, inventory)
        shared_directories.discard(host, inventory.topology, profile["instance_id"],
                                   entry["allocations"]["uid"], entry["allocations"]["gid"])
        cache = Path(inventory.specs[inventory.identity("http")].bindings.shared_directories[0].path)
        cache.parent.rmdir()
        feature_network.prepare_boot(host)
        feature_lifecycle.boot(host, entry, inventory)
        assert cache.is_mount(), "Boot did not prepare the cache before unit startup"
        for name in inventory.start_order(services.enabled_units(host, inventory.units)):
            host.run(["systemctl", "start", inventory.units[name]])
        feature_lifecycle.open_boot(host, entry, inventory)
        reported = json.loads(run([LAUNCHER, "report"], input=json.dumps(request(project, profile))).stdout)
        assert_shared_files(host, inventory, entry, reported)
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        second = run(["systemctl", "show", inventory.units[inventory.identity("relay")], "-p", "InvocationID", "--value"]).stdout
        for operation in ("down", "up"):
            changed = action(operation, request(project, profile, confirm=True, operation_id=str(uuid.uuid4())))
            assert changed["maintenance"]["down"] is (operation == "down")
            for identity, unit in inventory.units.items():
                active = run(["systemctl", "is-active", unit], check=False).stdout.strip()
                assert active == ("inactive" if operation == "down" and identity.role == COPIES_ROLE else "active")
            assert run(["systemctl", "show", inventory.units[inventory.identity("relay")], "-p", "InvocationID", "--value"]).stdout == second
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        profile, inventory, entry = numbered_count_changes(host, project, profile, inventory, entry, args.archive)
        assert action("stop", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
        for unit in inventory.units.values():
            assert run(["systemctl", "is-active", "--quiet", unit], check=False).returncode != 0
        assert not services.enabled_units(host, inventory.units)
        public_observation(host, topology, profile)
        restarted = action("restart", request(project, profile, operation_id=str(uuid.uuid4())))
        assert restarted["ok"] and restarted["status"] == "healthy", restarted
        _, entry = host.require_identity(project, profile)
        store, selection, inventory, _ = load(host, entry)
        assert set(services.enabled_units(host, inventory.units)) == set(inventory.units)
        assert set(selection.enabled_roles) == set(unit_selection_names(inventory.units))
        assert host.load_state(entry)["status"] == "healthy"
        assert services.wait_ready_set(host, inventory, inventory.units, time.monotonic() + 90)
        store.require_cleanup_clear()
        assert_numbered_copies(host, inventory, entry, len(inventory.copies(COPIES_ROLE)))
        public_observation(host, topology, profile, inventory=inventory, opened=True)
        assert action("stop", request(project, profile, operation_id=str(uuid.uuid4())))["status"] == "stopped"
        assert not services.enabled_units(host, inventory.units)
        public_observation(host, topology, profile)
        # Use the same cleanup owner as release transitions after confirmed stop.
        import shared_directories
        from feature_lifecycle import stop_units
        stop_units(host, inventory)
        shared_directories.discard(host, inventory.topology, profile["instance_id"],
                                   entry["allocations"]["uid"], entry["allocations"]["gid"])
        assert not Path(inventory.specs[inventory.identity("http")].bindings.shared_directories[0].path).exists()
        print("PASS host capabilities: package, artifact, admission, templates, private/public/Unix app bindings, verified wildcard confinement, shared files with reader EROFS and writer ENOSPC, disposable backup exclusion, stale-socket restart, public TCP via nginx stream, numbered copies 3->4->2->4->2->3 with consumers, retained secrets/state, copy operations, backup and rollback enablement, report, restart, maintenance continuity and stop closure")


if __name__ == "__main__":
    main()
