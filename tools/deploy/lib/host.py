"""Host filesystem/process boundary; test hosts inject paths and process results."""
from __future__ import annotations

from contextlib import contextmanager
import fcntl
import os
import signal
from pathlib import Path
import stat
import subprocess
import time

from contracts import (DeployError, INSTANCE_STATUSES, PREFIX, SCHEMA, HOST_PATHS, atomic_write, atomic_json, check_path,
                       identity, instance_paths, now, read_json, require, validate_attempt,
                       validate_registry, valid_timestamp, valid_uuid)

# Mutations queue behind one running deployment, backup or renewal. A deploy holds the
# host lock through unpack, backup, migration and readiness, which takes minutes on small hosts.
LOCK_WAIT_SECONDS = 600


class OperationTimeout(TimeoutError):
    """Interrupt blocking reads as well as checks between probes."""


def _late_alarm(signum, frame):
    """An alarm still in flight from a finished budget must not end the process."""


class wall_clock_budget:
    """Raise OperationTimeout in the block after `seconds` of wall-clock time.

    Linux deployment commands run bounded operations on their main thread. A socket
    timeout alone does not bound a peer that continuously drips response bytes.
    """

    def __init__(self, seconds):
        self.seconds = seconds
        self.closing = False

    def _restore(self):
        if self.closing:
            return
        self.closing = True
        signal.setitimer(signal.ITIMER_REAL, 0)
        # The default action would kill the process on an alarm generated before that call.
        handler = self.previous_handler
        signal.signal(signal.SIGALRM, handler if callable(handler) else _late_alarm)
        delay = 0
        if self.previous_timer[0]:
            # Preserve the caller's deadline and repeating interval. If its
            # deadline elapsed during collection, deliver it immediately after.
            delay = max(0.000001, self.previous_timer[0] - (time.monotonic() - self.started))
        signal.setitimer(signal.ITIMER_REAL, delay, self.previous_timer[1])

    def _exhausted(self, signum, frame):
        if self.closing:
            return
        # The alarm can land where Python would never call __exit__, so clean up first.
        self._restore()
        raise OperationTimeout("operation budget exhausted")

    def __enter__(self):
        self.previous_handler = signal.getsignal(signal.SIGALRM)
        self.previous_timer = signal.getitimer(signal.ITIMER_REAL)
        self.started = time.monotonic()
        signal.signal(signal.SIGALRM, self._exhausted)
        signal.setitimer(signal.ITIMER_REAL, self.seconds)
        return self

    def __exit__(self, *exception):
        self._restore()
        return False


class Paths:
    def __init__(self, root=Path("/")):
        self.root = Path(root).resolve()
        self.etc = self.p(HOST_PATHS["registry"]).parent
        self.state = self.p(HOST_PATHS["state"])
        self.tools = self.p(HOST_PATHS["tools"])
        self.registry = self.etc / "registry.json"

    def p(self, absolute):
        require(str(absolute).startswith("/") and ".." not in Path(absolute).parts, "unsafe_path", "Host paths must be absolute and bounded.")
        return self.root / str(absolute).lstrip("/")

    def instance_config(self, name):
        return self.p(instance_paths(name)["configuration"])

    def instance_state(self, name):
        return self.p(instance_paths(name)["state"])

    def opt(self, name):
        return self.p(instance_paths(name)["application"])

    def data(self, name):
        return self.p(instance_paths(name)["data"])


class Host:
    def __init__(self, paths=None):
        self.paths = paths or Paths()

    def run(self, args, input=None, env=None, timeout=30, check=True, cwd=None):
        # No inherited application settings, sudo/session variables or proxy.
        environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
        if env:
            environment.update(env)
        try:
            process = subprocess.Popen([str(a) for a in args], text=True,
                                       stdin=subprocess.PIPE if input is not None else None,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       env=environment, cwd=cwd, start_new_session=True)
            try:
                stdout, stderr = process.communicate(input=input, timeout=timeout)
            except BaseException:
                # A killed runuser/shell parent does not stop its descendants.
                # Own the session until cancellation has killed the whole group.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                process.stdout.close()
                process.stderr.close()
                if process.stdin is not None:
                    process.stdin.close()
                raise
            result = subprocess.CompletedProcess(args, process.returncode, stdout, stderr)
        except (OSError, subprocess.TimeoutExpired):
            raise DeployError("command_unavailable", "Required host command failed to start or exceeded its deadline.") from None
        if check and result.returncode:
            # Commands may include credentials or emit secrets on errors.
            raise DeployError("command_failed", f"Host operation failed ({Path(args[0]).name}); inspect it privately on the host.")
        return result

    def check_control_directory(self, path):
        """Check a deployment-owned directory, without inspecting /tmp/test ancestors."""
        path = check_path(path, "directory")
        if path.exists():
            info = path.stat()
            require(not info.st_mode & 0o022 and (info.st_uid == 0 or self.paths.root != Path("/")),
                    "control_directory_ownership", "Deployment control directories must be root-owned and not writable by other users.")
        return path

    def read_registry(self, missing=False):
        self.check_control_directory(self.paths.etc)
        path = check_path(self.paths.registry, "file")
        if not path.exists() and missing:
            return None
        require(path.exists(), "registry_missing", "This server has not been bootstrapped.")
        info = path.stat()
        require(info.st_uid == 0 if self.paths.root == Path("/") else True, "registry_owner", "Registry must be owned by root.")
        require(stat.S_IMODE(info.st_mode) == 0o644, "registry_mode", "Registry must have mode 0644.")
        return validate_registry(read_json(path))

    def write_registry(self, registry):
        validate_registry(registry)
        atomic_json(self.paths.registry, registry)

    def owned_write(self, path, content, mode=0o644, owner="root", group="root"):
        """Publish final owner/group/mode before making a control file visible."""
        if self.paths.root != Path("/"):
            owner, group = os.geteuid(), os.getegid()
        return atomic_write(path, content, mode, owner=owner, group=group)

    def journal(self, registry, entry=None):
        """Persist one public transition, incrementing the registry revision once."""
        registry["revision"] += 1
        registry["updated_at"] = now()
        if entry is not None:
            entry["updated_at"] = registry["updated_at"]
            registry["instances"][entry["profile"]["instance_id"]] = entry
        self.write_registry(registry)

    @contextmanager
    def lock(self, name, timeout=LOCK_WAIT_SECONDS, *, capacity_transition=False):
        require(name == "host" or valid_uuid(name), "invalid_lock", "Invalid deployment lock name.")
        path = check_path(self.paths.p(HOST_PATHS["locks"]) / ("host.lock" if name == "host" else f"instances/{name}.lock"), "file")
        path.parent.mkdir(parents=True, exist_ok=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            deadline = time.monotonic() + timeout
            while True:
                try:
                    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise DeployError("operation_locked", "Another operation holds this deployment lock; retry after it finishes.") from None
                    time.sleep(0.1)
            if name == "host" and not capacity_transition:
                # Recheck after acquiring the lock: a request may have passed
                # client validation before another process began resizing.
                from capacity import require_idle
                require_idle(self)
            yield
        finally:
            os.close(fd)

    def require_identity(self, project, profile):
        expected = identity(project, profile)
        registry = self.read_registry()
        require(registry["host_id"] == profile["host"]["host_id"], "host_mismatch", "SSH reached a different host identity; no changes made.")
        entry = registry["instances"].get(profile["instance_id"])
        require(entry is not None and all(entry[k] == v for k, v in expected.items() if k != "allocations") and all(entry["allocations"][k] == v for k,v in expected["allocations"].items()), "identity_mismatch", "Instance identity differs from this profile; no changes made.")
        require(entry["profile"] == profile, "profile_drift", "Shared environment profile differs from the host; run bootstrap check first.")
        return registry, entry

    def load_state(self, entry):
        for directory in (self.paths.state, self.paths.state / "instances", self.paths.instance_state(entry["name"])):
            self.check_control_directory(directory)
        path = check_path(self.paths.instance_state(entry["name"]) / "state.json", "file")
        if not path.exists():
            return {"schema": SCHEMA, "instance_id": entry["profile"]["instance_id"], "current": None, "previous": None,
                    "status": "never_deployed", "last_attempt": None, "history": [],
                    "configuration_revision": entry["configuration_revision"]}
        info = path.stat()
        require(stat.S_IMODE(info.st_mode) == 0o644 and (info.st_uid == 0 or self.paths.root != Path("/")),
                "state_ownership", "Deployment state must be root-owned with mode 0644.")
        state = self.validate_state(entry, read_json(path))
        if os.geteuid() == 0 or self.paths.root != Path("/"):
            from release import load_configuration_intents
            state = self.validate_state(entry, load_configuration_intents(self, entry, state))
        return state

    def validate_state(self, entry, state):
        # Load only at the operation boundary: release imports Host for deadlines,
        # while this validator reuses the existing manifest contract unchanged.
        from release import validate_manifest

        code, detail = "invalid_state", "Invalid deployment state; administrator reconciliation required."
        required = {"schema", "instance_id", "current", "previous", "status", "last_attempt", "history", "configuration_revision"}
        require(isinstance(state, dict) and required.issubset(state)
                and type(state["schema"]) is int and state["schema"] == SCHEMA and state["instance_id"] == entry["profile"]["instance_id"]
                and state["status"] in INSTANCE_STATUSES
                and type(state["configuration_revision"]) is int and state["configuration_revision"] >= 1
                and isinstance(state["history"], list) and len(state["history"]) <= 50
                and ("updated_at" not in state or valid_timestamp(state["updated_at"])), code, detail)
        for key in ("current", "previous"):
            if state[key] is not None:
                try:
                    validate_manifest(state[key], entry)
                except (DeployError, TypeError):
                    raise DeployError(code, detail) from None
        if "cutover" in state:
            from cutover_contracts import validate_progress
            validate_progress(state["cutover"])
        from compatibility import validate_state
        validate_state(state)
        if state["last_attempt"] is not None:
            validate_attempt(state["last_attempt"])
        for attempt in state["history"]:
            validate_attempt(attempt)
            require(attempt["outcome"] != "running", code, detail)
        return state

    def save_state(self, entry, state):
        path = check_path(self.paths.instance_state(entry["name"]) / "state.json", "file")
        if path.exists():
            previous = read_json(path).get("queue_compatibility")
            if previous is not None:
                from compatibility import validate
                validate(previous)
                candidate = state.get("queue_compatibility")
                require(candidate is not None, "queue_compatibility_invalid", "A persisted queue gate cannot be discarded.")
                validate(candidate)
                require(previous["floor"] != 3 or candidate["floor"] == 3,
                        "queue_compatibility_invalid", "The queue layout requirement cannot be lowered.")
        state["updated_at"] = now()
        self.validate_state(entry, state)
        from release import persist_configuration_intents, public_state
        persist_configuration_intents(self, entry, state)
        atomic_json(self.paths.instance_state(entry["name"]) / "state.json", public_state(state))
