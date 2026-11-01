#!/usr/bin/env python3
"""Exercise nginx reload recovery on an already deployed acceptance_host fixture.

Inside its explicitly owned, disposable Ubuntu/systemd Docker container:
  FOUNDRY_DISPOSABLE_HOST=1 python3 /verification/deploy/tests/acceptance_nginx.py --rounds 2

All three acceptance_host instances must already be healthy and use this exact
deployment tool bundle. Each round performs a real failed systemd ExecReload and
SIGKILLs bootstrap at four reload/acknowledgement boundaries. A temporary HTTP
header proves which configuration nginx is actually serving. Only this fixture's
selected site and temporary nginx drop-in are changed; cleanup restores them.
No application/shared-service restart, database reset, or Redis flush is used.
"""

# Moved from the upstream deploy harness; F4 must port this before acceptance.
if __name__ == "__main__":
    raise SystemExit("F4 acceptance port pending; this moved harness is not runnable yet.")

import argparse
import copy
import io
import json
import os
from pathlib import Path
import re
import runpy
import select
import signal
import subprocess
import sys
import time

sys.dont_write_bytecode = True
from acceptance_host import HOST_ID, TOOLS, WORK, Host, fixtures, snapshot
from topology import instance_units
from contracts import PREFIX, atomic_write, check_path, digest, identity
from provision import _template, _verify_tools, bootstrap_plan
from acceptance_http import https_response

HEADER = "X-Foundry-Acceptance-Reload"
DROP_IN = Path("/etc/systemd/system/nginx.service.d/foundry-acceptance-reload.conf")
DROP_IN_CONTENT = "# foundry disposable nginx acceptance only\n[Service]\nExecReload=\nExecReload=/usr/bin/false\n"
BOUNDARIES = ("before-reload", "after-reload", "after-ack", "after-ready")


def guard(host):
    if os.geteuid() != 0 or os.environ.get("FOUNDRY_DISPOSABLE_HOST") != "1" or not Path("/.dockerenv").is_file():
        raise RuntimeError("Run only inside the explicitly designated disposable Docker acceptance host")
    owned = check_path(WORK / "owned", "file")
    if not owned.exists() or owned.read_text() != "disposable acceptance fixture\n":
        raise RuntimeError("The acceptance_host ownership marker is missing or differs")
    host.check_control_directory(WORK)
    registry = host.read_registry()
    expected = {identity(project, profile)["name"]: (project, profile) for project, profile in fixtures()}
    if registry["host_id"] != HOST_ID or set(registry["instances"]) != set(expected):
        raise RuntimeError("Refusing a host outside the exact three-instance acceptance fixture")
    for name, (project, profile) in expected.items():
        _, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        if entry["provisioning"]["state"] != "ready" or state["status"] != "healthy" or state["current"] is None:
            raise RuntimeError("All acceptance instances must already be provisioned and deployed healthy")
    _verify_tools(host, registry, TOOLS)
    return registry["instances"]


def service(host, unit, properties):
    response = host.run(["systemctl", "show", unit, *["--property=" + key for key in properties]], timeout=10)
    values = dict(line.split("=", 1) for line in response.stdout.splitlines() if "=" in line)
    assert set(properties) <= set(values), "Incomplete systemd inspection"
    return values


def workers(host):
    pid = service(host, "nginx.service", ("MainPID",))["MainPID"]
    assert pid.isdigit() and int(pid) > 0, "nginx master must be running"
    children = Path(f"/proc/{pid}/task/{pid}/children").read_text().split()
    generation, draining = {}, False
    for child in children:
        try:
            command = Path(f"/proc/{child}/cmdline").read_bytes()
            if b"nginx: worker process" not in command:
                continue
            # PID plus Linux start ticks distinguishes a reused PID.
            stat = Path(f"/proc/{child}/stat").read_text().rsplit(")", 1)[1].split()
            generation[child] = stat[19]
            draining |= b"shutting down" in command
        except FileNotFoundError:
            draining = True
    return generation, draining


def settled_workers(host, previous=None):
    deadline, since, last = time.monotonic() + 20, None, None
    while time.monotonic() < deadline:
        current, draining = workers(host)
        if current and not draining and (previous is None or not set(current) & set(previous)):
            if current == last:
                if time.monotonic() - since >= 1:
                    return current
            else:
                since, last = time.monotonic(), current
        else:
            since, last = None, None
        time.sleep(0.1)
    raise RuntimeError("nginx workers did not reach a stable generation within 20 seconds")


def http_readiness(entry):
    status, headers, payload = https_response(entry, "/health/ready", 4096)
    assert status == 200, f"HTTPS readiness failed: instance={entry['name']} status={status}"
    assert json.loads(payload).get("ready") is True, f"Application did not report readiness: instance={entry['name']}"
    return headers.get(HEADER)


def header_label(value):
    # Only fixture-authored header values may enter diagnostics. An unexpected
    # application response must never accidentally print a private value.
    return value if value is None or re.fullmatch(r"round-[0-9]+-(?:failed-reload|before-reload|after-reload|after-ack|after-ready)", value) else "<unexpected>"


def wait_header(entry, expected):
    deadline = time.monotonic() + 20
    actual = None
    while time.monotonic() < deadline:
        actual = http_readiness(entry)
        if actual == expected:
            return
        time.sleep(0.1)
    raise RuntimeError(f"nginx header deadline: instance={entry['name']} expected={header_label(expected)!r} actual={header_label(actual)!r}")


def preserved(host, entries, selected):
    registry = copy.deepcopy(host.read_registry())
    registry.pop("updated_at")
    changed = registry["instances"][selected]
    changed.pop("updated_at")
    changed.pop("provisioning")
    postgres = registry["shared"]["postgresql"]
    units = [entry["unit"] for entry in entries.values()] + ["postgresql.service", "redis-server.service",
             f"postgresql@{postgres['version']}-{postgres['cluster']}.service", "foundry-acceptance-unrelated.service"]
    properties = ("MainPID", "NRestarts", "ActiveEnterTimestampMonotonic", "ExecMainStartTimestampMonotonic", "ActiveState", "SubState")
    services = {unit: service(host, unit, properties) for unit in units}
    assert all(value["ActiveState"] == "active" for value in services.values()), "A protected service is not active"
    nginx = service(host, "nginx.service", ("MainPID", "NRestarts", "ExecMainStartTimestampMonotonic"))
    files = {}
    for name, entry in entries.items():
        paths = [host.paths.instance_config(name) / "secrets.json", host.paths.instance_config(name) / "app.env",
                 host.paths.instance_state(name) / "state.json"]
        if name != selected:
            paths.append(host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{name}.conf"))
        files[name] = [digest(path) for path in paths]
    return registry, services, nginx, files


def check_preserved(host, entries, selected, baseline, header=None):
    assert preserved(host, entries, selected) == baseline, "Resources, credentials, application state, or unrelated services changed"
    for name, entry in entries.items():
        expected, actual = header if name == selected else None, http_readiness(entry)
        assert actual == expected, f"Unexpected live site header: instance={name} expected={header_label(expected)!r} actual={header_label(actual)!r}"


def bootstrap(host, project, profile, expect_failure=False):
    request = {"project": project, "profile": profile}
    command = [sys.executable, str(TOOLS / "server/manage.py")]
    plan = json.loads(host.run(command + ["bootstrap-plan"], input=json.dumps(request), timeout=120).stdout)
    assert plan.get("ok") is True, "Live bootstrap inspection unexpectedly refused"
    request.update(confirm=True, plan_token=plan["plan_token"])
    result = host.run(command + ["bootstrap-apply"], input=json.dumps(request), timeout=180, check=False)
    answer = json.loads(result.stdout)
    if expect_failure:
        assert result.returncode == 1 and answer.get("error", {}).get("code") == "command_failed", "Expected an actual failed systemd reload"
    else:
        assert result.returncode == 0 and answer.get("ok") is True, "Bootstrap recovery failed"
    return answer


def failed_reload(host):
    values = service(host, "nginx.service", ("ActiveState", "ReloadResult", "ExecReload"))
    assert values["ActiveState"] == "active" and values["ReloadResult"] == "exit-code", "nginx must remain active after an actual ExecReload exit-code failure"
    execution = values["ExecReload"]
    assert re.search(r"\bpath=/usr/bin/false\s*;", execution), "Failed reload did not execute the fixture's /usr/bin/false"
    assert re.search(r"\bcode=exited\s*;", execution) and re.search(r"\bstatus=1(?:\s*;|\s*\})", execution), "Fixture ExecReload did not exit with status 1"


def repeat_noop(host, entries, selected, baseline):
    before, stable = snapshot(host), settled_workers(host)
    for project, profile in fixtures():
        assert bootstrap(host, project, profile).get("unchanged") is True, "Repeated bootstrap must report unchanged"
    assert settled_workers(host) == stable, "Repeated bootstrap unexpectedly reloaded nginx workers"
    assert snapshot(host) == before, "Repeated bootstrap changed registry or credentials"
    check_preserved(host, entries, selected, baseline)


def pending(host, name, expected_state):
    provisioning = host.read_registry()["instances"][name]["provisioning"]
    assert provisioning["state"] == expected_state and "site" not in provisioning["steps"], "Reload completion was incorrectly acknowledged"


def acknowledged(host, name, state="ready"):
    provisioning = host.read_registry()["instances"][name]["provisioning"]
    assert provisioning["state"] == state and "site" in provisioning["steps"], f"Reload acknowledgement differs: instance={name} expected_state={state}"


def clear_drop_in(host):
    check_path(DROP_IN, "file")
    if DROP_IN.exists():
        assert DROP_IN.read_text() == DROP_IN_CONTENT, "Refusing to remove a foreign nginx override"
        DROP_IN.unlink()
        host.run(["systemctl", "daemon-reload"])


def load_drift(host, site, canonical, entry, value):
    assert site.read_text() == canonical, "Each scenario must begin from the canonical owned site"
    old = settled_workers(host)
    anchor = '    add_header Strict-Transport-Security "max-age=31536000" always;'
    assert canonical.count(anchor) == 1 and HEADER not in canonical, "Unexpected canonical nginx template"
    atomic_write(site, canonical.replace(anchor, anchor + f'\n    add_header {HEADER} "{value}" always;'))
    host.run(["nginx", "-t", "-q"])
    host.run(["systemctl", "reload", "nginx"])
    current = settled_workers(host, previous=old)
    wait_header(entry, value)
    return current


class BoundaryHost(Host):
    """A test process barrier around real subprocesses/durable registry writes."""
    def __init__(self, boundary, ready_fd, name):
        super().__init__()
        self.boundary, self.ready_fd, self.name = boundary, ready_fd, name
        self.reloaded = False

    def stop_at(self, boundary):
        if self.boundary == boundary:
            os.write(self.ready_fd, (boundary + "\n").encode())
            while True:
                signal.pause()  # Parent must SIGKILL this real process.

    def run(self, args, **kwargs):
        reload = list(args) == ["systemctl", "reload", "nginx"]
        if reload:
            self.stop_at("before-reload")
        result = super().run(args, **kwargs)
        if reload:
            self.reloaded = True
            self.stop_at("after-reload")
        return result

    def write_registry(self, registry):
        super().write_registry(registry)
        provisioning = registry["instances"].get(self.name, {}).get("provisioning", {})
        if self.reloaded and "site" in provisioning.get("steps", []):
            if provisioning.get("state") == "preparing":
                self.stop_at("after-ack")
            elif provisioning.get("state") == "ready":
                self.stop_at("after-ready")


def child(boundary, ready_fd):
    host = Host()
    guard(host)
    project, profile = fixtures()[0]
    plan = bootstrap_plan(host, project, profile)
    assert plan.get("ok") is True
    request = {"project": project, "profile": profile, "confirm": True, "plan_token": plan["plan_token"]}
    # Invoke the unchanged public JSON entrypoint. Only its Host constructor is
    # replaced inside this disposable child process; commands still run for real.
    import host as host_module
    host_module.Host = lambda: BoundaryHost(boundary, ready_fd, identity(project, profile)["name"])
    sys.stdin = io.TextIOWrapper(io.BytesIO(json.dumps(request).encode()))
    sys.argv = [str(TOOLS / "server/manage.py"), "bootstrap-apply"]
    runpy.run_path(sys.argv[0], run_name="__main__")


def kill_at(boundary, observe):
    read_fd, write_fd = os.pipe()
    process = None
    try:
        process = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--child", boundary,
                                    "--ready-fd", str(write_fd)], pass_fds=(write_fd,),
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        os.close(write_fd)
        write_fd = None
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            readable, _, _ = select.select([read_fd], [], [], 0.2)
            if readable:
                assert os.read(read_fd, 128).decode().strip() == boundary, "Child exited before its requested boundary"
                observe()
                process.kill()
                assert process.wait(timeout=10) == -signal.SIGKILL, "Bootstrap was not killed at its boundary"
                return
            assert process.poll() is None, "Bootstrap child terminated before its boundary"
        raise RuntimeError("Bootstrap child did not reach its boundary within 180 seconds")
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait(timeout=10)
        os.close(read_fd)
        if write_fd is not None:
            os.close(write_fd)


def run(rounds):
    host = Host()
    entries = guard(host)
    project, profile = fixtures()[0]
    name = identity(project, profile)["name"]
    entry = entries[name]
    site = check_path(host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{name}.conf"), "file")
    canonical = _template(TOOLS, "nginx-https.conf", entry)
    assert site.read_text() == canonical, "Selected fixture site must start canonical"
    assert not DROP_IN.exists() and not DROP_IN.is_symlink(), "Refusing an existing nginx acceptance override"
    baseline = preserved(host, entries, name)
    check_preserved(host, entries, name, baseline)
    with host.lock("acceptance-nginx", timeout=0):
        try:
            for round_number in range(1, rounds + 1):
                value = f"round-{round_number}-failed-reload"
                old = load_drift(host, site, canonical, entry, value)
                try:
                    check_path(DROP_IN.parent, "directory")
                    atomic_write(DROP_IN, DROP_IN_CONTENT)
                    host.run(["systemctl", "daemon-reload"])
                    bootstrap(host, project, profile, expect_failure=True)
                    failed_reload(host)
                    assert site.read_text() == canonical
                    pending(host, name, "failed")
                    assert settled_workers(host) == old, "Failed ExecReload unexpectedly replaced workers"
                    check_preserved(host, entries, name, baseline, value)
                finally:
                    clear_drop_in(host)
                bootstrap(host, project, profile)
                settled_workers(host, previous=old)
                wait_header(entry, None)
                acknowledged(host, name)
                repeat_noop(host, entries, name, baseline)
                print(f"PASS round={round_number} scenario=failed-reload recovery=ready repeats=unchanged protected=preserved", flush=True)

                for boundary in BOUNDARIES:
                    value = f"round-{round_number}-{boundary}"
                    old = load_drift(host, site, canonical, entry, value)
                    def observe():
                        assert site.read_text() == canonical
                        if boundary == "after-ack":
                            acknowledged(host, name, "preparing")
                        elif boundary == "after-ready":
                            acknowledged(host, name)
                        else:
                            pending(host, name, "preparing")
                        # A successful reload is asynchronous: both worker
                        # generations can accept new connections while draining.
                        # Observe HTTP only after the old generation has exited.
                        if boundary != "before-reload":
                            settled_workers(host, previous=old)
                        wait_header(entry, value if boundary == "before-reload" else None)
                        check_preserved(host, entries, name, baseline, value if boundary == "before-reload" else None)
                    kill_at(boundary, observe)
                    before_retry = settled_workers(host, previous=old if boundary != "before-reload" else None)
                    answer = bootstrap(host, project, profile)
                    if boundary in {"after-ack", "after-ready"}:
                        if boundary == "after-ready":
                            assert answer.get("unchanged") is True
                        assert settled_workers(host) == before_retry, "Acknowledged reload was repeated"
                    else:
                        settled_workers(host, previous=before_retry)
                    wait_header(entry, None)
                    acknowledged(host, name)
                    repeat_noop(host, entries, name, baseline)
                    print(f"PASS round={round_number} scenario={boundary} signal=SIGKILL recovery=ready repeats=unchanged protected=preserved", flush=True)
        finally:
            clear_drop_in(host)
            # Restore only the fixture header, including a failed reload whose
            # canonical file is already on disk but whose old workers still run.
            if site.read_text() != canonical or http_readiness(entry) is not None:
                atomic_write(site, canonical)
                host.run(["nginx", "-t", "-q"])
                host.run(["systemctl", "reload", "nginx"])
                wait_header(entry, None)
        check_preserved(host, entries, name, baseline)
    print(f"PASS rounds={rounds} scenarios={rounds * (len(BOUNDARIES) + 1)} cleanup=restored secrets=unreported", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rounds", type=int, default=2, help="Repeat all five recovery scenarios (1–10; default 2)")
    parser.add_argument("--child", choices=BOUNDARIES, help=argparse.SUPPRESS)
    parser.add_argument("--ready-fd", type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    if not 1 <= args.rounds <= 10:
        parser.error("--rounds must be between 1 and 10")
    if args.child:
        if args.ready_fd is None or args.ready_fd < 3:
            parser.error("the internal child requires its notification pipe")
        child(args.child, args.ready_fd)
    else:
        run(args.rounds)


if __name__ == "__main__":
    main()
