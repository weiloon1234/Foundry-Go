#!/usr/bin/env python3
"""Opt-in generation-12 preview scenario inside the existing disposable harness.

Use the ordinary deploy-acceptance archive. The artifact is a second, pinned copy
of that fixture's Linux artisan binary; no artifact network download is needed.
Lane B's archive verification and publication remain production code paths.
"""
from dataclasses import asdict
import argparse
import copy
import hashlib
import io
import json
from pathlib import Path
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
    checksum = hashlib.sha256(files["artisan"]).hexdigest()
    platform = "linux-" + profile["architecture"]
    topology = parse_topology({"schema": 2, "roles": [
        {"name": "http", "kind": "builtin", "requires": ["relay"], "packages": ["coreutils"],
         "bindings": {"relay": {"endpoint": {"role": "relay", "listener": "control", "scheme": "http"}},
                      "checksum": {"executable": {"package": "coreutils", "path": "/usr/bin/sha256sum"}}}},
        {"name": "relay", "kind": "executable", "executable": {"artifact": "relay", "entry": "server"},
         "arguments": [{"literal": "feature:relay"}, {"listener_address": "control"}, {"listener_port": "control"}],
         "listeners": {"control": {"transport": "tcp", "bind": "loopback"}},
         "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200, "body": "feature-relay-ready"},
         "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
         "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
         "maintenance": "stop", "state": {"kind": "disposable"}}],
        "artifacts": {"relay": {"version": "fixture-1", "format": "file", "executables": {"server": "relay"},
            "platforms": {platform: {"url": "https://acceptance.invalid/relay", "sha256": checksum}}}}})
    profile = parse_profile(profile, topology).projection()
    payload_path = "deploy/artifacts/relay/" + checksum + ".bin"
    files[payload_path] = files["artisan"]
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
    shared.prepare(host, (project, profile), private)
    result = action("activate", request(project, profile, archive=str(archive), archive_sha256=digest(archive), operation_id=str(uuid.uuid4())))
    assert result["ok"] and result["preview"]
    from feature_lifecycle import load
    _, entry = host.require_identity(project, profile)
    _, _, inventory, _ = load(host, entry)
    for unit in inventory.units.values():
        assert run(["systemctl", "is-active", unit]).stdout.strip() == "active"
    # HTTP readiness calls the relay using deployment.LoadBindings, proving app delivery.
    import services
    import time
    assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
    first = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
    assert action("restart", request(project, profile, operation_id=str(uuid.uuid4())))["ok"]
    second = run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout
    assert first.strip() and second.strip() and first != second
    assert services.wait_ready(host, inventory, "http", time.monotonic() + 90)
    for operation in ("down", "up"):
        refused = run([LAUNCHER, operation], input=json.dumps(request(project, profile)), check=False)
        assert json.loads(refused.stdout)["error"]["code"] == "capability_unavailable"
        for unit in inventory.units.values():
            assert run(["systemctl", "is-active", unit]).stdout.strip() == "active"
        assert run(["systemctl", "show", inventory.units["relay"], "-p", "InvocationID", "--value"]).stdout == second
    print("PASS feature preview: package, artifact, admission, app binding, restart and symmetric maintenance refusal")


if __name__ == "__main__":
    main()
