#!/usr/bin/env python3
"""Scheduler/ACL, explicit rollback, interrupted switch, and config acceptance."""
import argparse
import base64
import copy
import io
import json
import os
from pathlib import Path
import runpy
import select
import signal
import subprocess
import sys
import tarfile
import time
import uuid

from acceptance_support import (ROOTS, WORK, action, fixture, guard, postgres_denial,
                                postgres_settings, private_json, request, run, service, wait_for)


def redis(host, entry, *args, admin=False):
    shared = host.read_registry()["shared"]["redis"]
    if admin:
        # CONTRACT: F0 private host-secrets.json retains inherited redis_password.
        secrets = json.loads(Path("/etc/foundry/host-secrets.json").read_text())
        username, password = shared["admin_user"], secrets["redis_password"]
    else:
        secrets = json.loads((host.paths.instance_config(entry["name"]) / "secrets.json").read_text())
        username, password = entry["allocations"]["redis_user"], secrets["redis_password"]
    result = run(["redis-cli", "-3", "--json", "--tls", "--cacert", ROOTS,
                  "-h", "localhost", "-p", str(shared["port"]), "--user", username, *args],
                 env={"REDISCLI_AUTH": password})
    assert not result.stdout.startswith("error:"), "Redis command refused"
    return json.loads(result.stdout)


def scheduler(host, entry):
    from contracts import instance_units
    unit = instance_units(entry)["scheduler"]
    service(unit)
    grants = entry["redis_grants"]["keys"]
    # CONTRACT: literal inventory prefixes include a named coordination child
    # and portal cache child. No broad wildcard is accepted by this fixture.
    coordination = [prefix for prefix in grants if prefix.startswith("foundry:lease:") and ".coordination:" in prefix]
    portal = [prefix for prefix in grants if prefix.startswith("foundry:cache:") and ".portal:" in prefix]
    assert coordination and portal, "Archive must declare scheduler coordination and portal cache"
    assert not any(any(char in prefix for char in "*?[]\\") for prefix in grants)
    namespace = entry["allocations"]["namespace"]
    for prefix in coordination + portal:
        parts = prefix.split(":")
        assert parts[3].startswith(namespace["application"] + ".") and parts[4] == namespace["environment"]
    # Intentional denial tests in acceptance_host precede this isolated window.
    # Reset only the Foundry ACL diagnostics, never foreign Redis or its data.
    redis(host, entry, "ACL", "LOG", "RESET", admin=True)
    start = str(int(time.time()))

    def keys_present():
        for prefix in coordination:
            cursor = "0"
            for _ in range(100):
                cursor, keys = redis(host, entry, "SCAN", cursor, "MATCH", prefix + "*", "COUNT", "100", admin=True)
                for key in keys:
                    assert key.startswith(prefix)
                    if redis(host, entry, "PTTL", key, admin=True) > 0:
                        return True
                if str(cursor) == "0":
                    break
        return False

    wait_for(keys_present, "No live coordination lease in the granted namespace")
    for prefix in portal:
        key = prefix + "acceptance:" + uuid.uuid4().hex
        assert redis(host, entry, "SET", key, "portal-cache-roundtrip", "EX", "120") == "OK"
        assert redis(host, entry, "GET", key) == "portal-cache-roundtrip"
        assert redis(host, entry, "DEL", key) == 1

    # CONTRACT: the supplied app has a <=60s fixture schedule whose successful
    # handler emits this JSON message (override for an existing app's schedule).
    # Unit activity and a leader lease alone do not prove an occurrence happened.
    message = os.environ.get("ACCEPTANCE_SCHEDULE_MESSAGE", "acceptance schedule completed")
    def occurred():
        lines = run(["journalctl", "--unit", unit, "--since", "@" + start,
                     "--no-pager", "--output=cat", "--lines=2000"]).stdout.splitlines()
        for line in lines:
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if isinstance(event, dict) and event.get("msg") == message:
                return True
        return False
    wait_for(occurred, "Scheduler did not complete a fresh occurrence", timeout=150)
    assert redis(host, entry, "ACL", "LOG", "128", admin=True) == [], "Redis ACL denials occurred during role/cache acceptance"
    service(unit)
    print("PASS scheduler occurrence, expiring coordination lease, portal-cache commands and empty ACL LOG")


def running(entry):
    from contracts import instance_units
    result = {}
    for role, unit in instance_units(entry).items():
        process = service(unit)
        result[role] = {**process, "exe": os.readlink(f"/proc/{process['pid']}/exe")}
    return result


def selected(host, entry):
    # Canonical release paths come from the F0 path owner.
    return (host.paths.opt(entry["name"]) / "current").resolve(strict=True)


def prove_running(host, entry):
    expected = str(selected(host, entry) / "artisan")
    processes = running(entry)
    assert all(item["exe"] == expected for item in processes.values()), "A running role uses a different release executable"
    import release
    assert release.ready(host, entry), "Selected release is not ready"
    assert host.load_state(entry)["current"]["version"] == selected(host, entry).name
    return processes


def second_archive(source, project, profile):
    from acceptance_host import archive_for
    # CONTRACT: reuse inherited fixture-only archive identity rebinding. A second
    # valid release ID and renamed private-source-map directory reuse the input
    # binary; this checks lifecycle/executable ownership, not code differences.
    source = archive_for(source, project, profile)
    destination = WORK / ("second-" + uuid.uuid4().hex + ".tar.gz")
    with tarfile.open(source, "r:gz") as incoming:
        manifest = json.load(incoming.extractfile("manifest.json"))
        old = manifest["version"]
        version = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + uuid.uuid4().hex
        def rename(name):
            return name.replace("private-source-maps/" + old, "private-source-maps/" + version, 1)
        manifest["version"] = version
        manifest["files"] = {rename(name): checksum for name, checksum in manifest["files"].items()}
        with tarfile.open(destination, "w:gz") as outgoing:
            for original in incoming:
                item = copy.copy(original)
                item.name = rename(item.name)
                if original.name == "manifest.json":
                    data = json.dumps(manifest).encode()
                    item.size = len(data)
                    outgoing.addfile(item, io.BytesIO(data))
                else:
                    outgoing.addfile(item, incoming.extractfile(original) if original.isfile() else None)
    return destination, version


def config_edit(host, project, profile, entry):
    from contracts import parse_env
    import release
    path = host.paths.instance_config(entry["name"]) / "app.env"
    before = parse_env(path.read_text())
    # CONTRACT: fixture archive uses application.Settings under APP, including
    # generated encryption settings. Bootstrap MUST initialize both active fields.
    key_id, key, previous = ("APP__ENCRYPTION__" + suffix for suffix in ("KEY_ID", "KEY", "PREVIOUS"))
    assert before.get(key_id), "Bootstrap omitted encryption key ID"
    def material(values, name):
        if values.get(name + "_FILE"):
            return Path(values[name + "_FILE"]).read_text().strip()
        return values.get(name, "")
    original = material(before, key)
    assert len(base64.urlsafe_b64decode(original + "=")) == 32, "Bootstrap key is not initialized"
    revision = entry["configuration_revision"]
    old_ring = json.loads(material(before, previous) or "{}")
    old_ring[before[key_id]] = {"key": original}
    changes = {key_id: "acceptance_" + uuid.uuid4().hex[:12],
               key: base64.urlsafe_b64encode(os.urandom(32)).decode().rstrip("="),
               previous: json.dumps(old_ring)}
    # CONTRACT: config-set takes <=16 values atomically; direct writes supersede
    # the corresponding _FILE alias through the production config owner.
    action("config-set", request(project, profile, values=changes))
    _, updated = host.require_identity(project, profile)
    assert updated["configuration_revision"] == revision + 1
    after = parse_env(path.read_text())
    assert after[key_id] == changes[key_id]
    assert material(after, key) == changes[key]
    assert json.loads(material(after, previous)) == old_ring
    release.app(host, updated, selected(host, updated), "config:check")
    prove_running(host, updated)
    print("PASS bootstrap keyring initialization and atomic three-key rotation retaining the old key")
    return updated


def child(payload_file, ready_fd):
    guard()
    from acceptance_host import TOOLS
    import release
    original = release.switch_to
    # CONTRACT: F0 release.switch_to(host, entry, version) publishes current and
    # returns before restart. Patch only this disposable child, never source files.
    def barrier(*args, **kwargs):
        original(*args, **kwargs)
        os.write(ready_fd, b"switched\n")
        while True:
            signal.pause()
    release.switch_to = barrier
    sys.stdin = io.StringIO(Path(payload_file).read_text())
    sys.argv = [str(TOOLS / "server/manage.py"), "activate"]
    runpy.run_path(sys.argv[0], run_name="__main__")


def interrupt_switch(payload, observe):
    payload_file = WORK / ("interruption-" + uuid.uuid4().hex + ".json")
    private_json(payload_file, payload)
    read_fd, write_fd = os.pipe()
    process = None
    try:
        process = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--child", str(payload_file),
                                    "--ready-fd", str(write_fd)], pass_fds=(write_fd,),
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        os.close(write_fd)
        write_fd = None
        deadline = time.monotonic() + 300
        while time.monotonic() < deadline:
            readable, _, _ = select.select([read_fd], [], [], 0.2)
            if readable:
                assert os.read(read_fd, 128) == b"switched\n", "Child exited before current-switch barrier"
                observe()
                process.kill()
                assert process.wait(timeout=10) == -signal.SIGKILL
                return
            assert process.poll() is None, "Activation exited before interruption"
        raise AssertionError("Activation never reached the current-switch barrier")
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait(timeout=10)
        os.close(read_fd)
        if write_fd is not None:
            os.close(write_fd)
        payload_file.unlink()


def main(archive):
    guard()
    host, project, profile, entry = fixture()
    from contracts import digest
    own = postgres_settings(host, entry)
    others = [other["allocations"]["database"] for other in host.read_registry()["instances"].values()
              if other["allocations"]["database"] != own["database"]]
    assert others, "T1 requires another provisioned acceptance database"
    for database in others:
        postgres_denial(own, database)
    print("PASS T1 verified-TLS own-database controls around each authorization denial")
    scheduler(host, entry)
    entry = config_edit(host, project, profile, entry)
    original = selected(host, entry).name
    candidate, version = second_archive(archive, project, profile)
    activate = request(project, profile, archive=str(candidate), archive_sha256=digest(candidate))
    action("activate", activate)
    assert selected(host, entry).name == version
    prove_running(host, entry)
    # CONTRACT: F0 explicit target + expected_current + operation_id; replaying
    # the SAME operation after its expected_current changed is an unchanged success.
    rollback = request(project, profile, target=original, expected_current=version)
    action("rollback", rollback)
    assert selected(host, entry).name == original
    before = (host.load_state(entry), prove_running(host, entry), host.paths.registry.read_bytes())
    repeated = action("rollback", rollback)
    assert repeated.get("unchanged") is True
    assert (host.load_state(entry), prove_running(host, entry), host.paths.registry.read_bytes()) == before
    print("PASS explicit-target rollback and identical replay preserve release, state, registry and PIDs")
    old_http = before[1]["http"]
    activate["operation_id"] = str(uuid.uuid4())
    def observe():
        assert selected(host, entry).name == version
        assert os.readlink(f"/proc/{old_http['pid']}/exe") == old_http["exe"], "Fixture did not reach old-process/new-current state"
    interrupt_switch(activate, observe)
    action("reconcile", request(project, profile))
    assert selected(host, entry).name == version
    recovered = prove_running(host, entry)
    assert recovered["http"]["pid"] != old_http["pid"]
    print("PASS SIGKILL after current switch; reconcile runs every role from the selected release")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--child", type=Path, help=argparse.SUPPRESS)
    parser.add_argument("--ready-fd", type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.child:
        child(args.child, args.ready_fd)
    else:
        parser.error("--archive is required") if args.archive is None else main(args.archive)
