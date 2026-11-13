"""Feature start authority. Enablement and network exposure have separate owners.

Unresolved independent operation cleanup denies validation and ordinary starts.
Feature helper bundles remain retained until selective lifetime integration.
No start helper acquires the host lock. Writers take the short instance lock;
readers hold it shared, then serialize claims with a short per-unit lock.
"""
from contextlib import contextmanager
from dataclasses import replace
import fcntl
import os
import re
from pathlib import Path
import time

from contracts import (AdmissionDisposition, ClosureEvidence, ClosureStage,
                       DeploymentSelection, HelperInvocation, RuntimeResolution,
                       ValidationPermit, atomic_json, atomic_write, check_path,
                       contract_uuid, read_json, record_document, require, validate_attempt)

VALIDATION_PHASES = frozenset({"health"})
TERMINAL_OUTCOMES = frozenset({"succeeded", "failed", "failed_applied", "reconciled"})
MAX_PERMITS = 128
MAX_ADMITTED_INVOCATIONS = 64


def protected_path(host, path, kind):
    """Control ancestry is never writable by the service account."""
    path = check_path(path, kind)
    for parent in [path, *path.parents]:
        if parent == host.paths.root:
            break
        if parent.exists():
            info = parent.stat()
            require(not info.st_mode & 0o022 and
                    (info.st_uid == 0 or host.paths.root != Path("/")),
                    "admission_ownership", "Admission evidence must be owned and protected.")
    return path


def read_owned(host, path, parser):
    path = protected_path(host, path, "file")
    require(path.exists(), "admission_missing", "Required admission evidence is missing.")
    return parser(read_json(path))


def protected_directory(host, path, mode=0o755):
    """Persist every new ancestry link, not only the final file's parent."""
    path = protected_path(host, path, "directory")
    missing, parent = [], path
    while not parent.exists():
        missing.append(parent)
        parent = parent.parent
    for directory in reversed(missing):
        try:
            directory.mkdir(mode=0o700)
        except FileExistsError:
            protected_path(host, directory, "directory")
            continue
        fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            uid, gid = (0, 0) if host.paths.root == Path("/") else (os.geteuid(), os.getegid())
            os.fchown(fd, uid, gid)
            os.fchmod(fd, mode if directory == path else 0o755)
            os.fsync(fd)
        finally:
            os.close(fd)
        fd = os.open(directory.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    return path


def boot_id(host):
    return contract_uuid(host.paths.p("/proc/sys/kernel/random/boot_id").read_text().strip())


def executor_alive(host, expected):
    """Reject reuse, zombies and a different systemd invocation; never format proc data."""
    root = host.paths.p("/proc") / str(expected.pid)
    try:
        before = (root / "stat").read_text()
        fields = before[before.rfind(")") + 2:].split()
        environment = (root / "environ").read_bytes()
        after = (root / "stat").read_text()
        after_fields = after[after.rfind(")") + 2:].split()
        return (fields[0] not in ("Z", "X") and after_fields[0] not in ("Z", "X") and
                int(fields[19]) == int(after_fields[19]) == expected.start_ticks and
                b"INVOCATION_ID=" + expected.invocation_id.encode() in environment.split(b"\0"))
    except (OSError, ValueError, IndexError):
        return False


class AdmissionStore:
    def __init__(self, host, instance_id):
        self.host = host
        self.instance_id = contract_uuid(instance_id)
        self.durable = host.paths.state / "admission" / self.instance_id
        self.runtime = host.paths.p("/run/foundry/admission") / self.instance_id

    @contextmanager
    def _lock(self, name="selection", *, shared=False):
        path = protected_path(self.host, self.runtime / (name + ".lock"), "file")
        protected_directory(self.host, path.parent, 0o700)
        fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            deadline = time.monotonic() + 10
            while True:
                try:
                    fcntl.flock(fd, (fcntl.LOCK_SH if shared else fcntl.LOCK_EX) | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    require(time.monotonic() < deadline, "admission_busy", "Start coordination is busy; retry after reconciliation.")
                    time.sleep(.01)
            yield
        finally:
            os.close(fd)

    def _write(self, path, record):
        protected_path(self.host, path, "file")
        protected_directory(self.host, path.parent, 0o700)
        atomic_json(path, record_document(record), mode=0o600)

    def record_operation(self, attempt):
        """Lifecycle journal handoff, retaining every unresolved operation."""
        validate_attempt(attempt)
        operation = contract_uuid(attempt.get("operation_id"))
        with self._lock():
            path = protected_path(self.host, self.durable / "operations" / (operation + ".json"), "file")
            protected_directory(self.host, path.parent, 0o700)
            atomic_json(path, attempt, mode=0o600)

    def _operations(self):
        directory = protected_path(self.host, self.durable / "operations", "directory")
        require(directory.exists(), "admission_journal", "Admission requires operation evidence.")
        paths = list(directory.iterdir())
        require(len(paths) <= 1024, "admission_journal", "Reconcile retained operation evidence before starting.")
        result = {}
        for path in paths:
            require(path.suffix == ".json", "admission_journal", "Unknown operation evidence.")
            operation = contract_uuid(path.stem)
            attempt = read_owned(self.host, path, validate_attempt)
            require(attempt.get("operation_id") == operation, "admission_journal", "Operation identity mismatch.")
            result[operation] = attempt
        return result

    def cleanup_status(self, operation, invocation, status):
        require(status in ("reserved", "armed", "pending", "closed", "complete") and
                isinstance(invocation, str) and re.fullmatch(r"[a-f0-9]{32}", invocation),
                "operation_cleanup", "Invalid cleanup owner state.")
        with self._lock():
            path = self.durable / "cleanup" / (contract_uuid(operation) + ".json")
            protected_directory(self.host, path.parent, 0o700)
            if path.exists():
                prior = read_owned(self.host, path, lambda v: v)
                require(prior["invocation_id"] == invocation or prior["status"] == "reserved" and prior["invocation_id"] == "0" * 32,
                        "operation_cleanup", "Cleanup invocation cannot change.")
            atomic_json(path, {"schema": 1, "operation_id": operation, "invocation_id": invocation, "status": status}, mode=0o600)

    def require_cleanup_clear(self, validation_operation=None):
        directory = protected_path(self.host, self.durable / "cleanup", "directory")
        if not directory.exists():
            return
        paths = list(directory.iterdir())
        require(len(paths) <= 1024, "operation_cleanup", "Reconcile retained cleanup owners before starting.")
        for path in paths:
            value = read_owned(self.host, path, lambda v: v)
            require(path.name == contract_uuid(value["operation_id"]) + ".json" and value.get("schema") == 1 and
                    value.get("status") in ("reserved", "armed", "pending", "closed", "complete") and
                    isinstance(value.get("invocation_id"), str) and re.fullmatch(r"[a-f0-9]{32}", value["invocation_id"]),
                    "operation_cleanup", "Invalid cleanup evidence.")
            require(value["status"] == "complete" or
                    value["status"] in ("reserved", "armed") and value["operation_id"] == validation_operation,
                    "operation_cleanup_pending", "An independent cleanup owner remains unresolved; starts are blocked.")

    def _matching(self, selection, resolution):
        a = selection.admission
        require(selection.instance_id == self.instance_id and a.resolution_id == resolution.id
                and a.deployment_sha256 == resolution.deployment_sha256
                and a.private_revision == resolution.private_revision
                and set(selection.enabled_roles) <= set(resolution.roles)
                and set(selection.maintenance_holds) <= set(resolution.roles),
                "admission_selection", "Admission does not match the selected runtime resolution.")

    def begin(self, selection, resolution):
        """Call after recording intent, before any stop/migration/selection effect."""
        selection = DeploymentSelection.parse(record_document(selection))
        resolution = RuntimeResolution.parse(record_document(resolution))
        self._matching(selection, resolution)
        require(selection.admission.disposition is AdmissionDisposition.PENDING,
                "admission_pending_required", "Transitions must revoke committed admission first.")
        with self._lock():
            operations = self._operations()
            require(selection.admission.operation_id in operations and
                    operations[selection.admission.operation_id]["outcome"] == "running",
                    "admission_journal", "Pending admission requires durable running intent.")
            # Any crash between these writes fails closed through the journal or mismatch.
            self._write(self.durable / "selection.json", selection)
            self._write(self.durable / "resolution.json", resolution)

    def record_boot_closure(self, evidence):
        evidence = ClosureEvidence.parse(record_document(evidence))
        require(evidence.boot_id == boot_id(self.host) and
                evidence.stage is ClosureStage.APPLICATION_CLOSED and
                all(u.instance_id == self.instance_id for u in evidence.units),
                "admission_closed_required", "Boot preparation requires complete current-boot closure.")
        with self._lock():
            self._write(self.runtime / "boot.json", evidence)

    def issue_permit(self, permit, evidence):
        permit = ValidationPermit.parse(record_document(permit))
        evidence = ClosureEvidence.parse(record_document(evidence))
        with self._lock():
            selection, resolution = self._selection()
            permit.require_closure(evidence, resolution)
            self._validation(selection, permit)
            require(all(u.instance_id == self.instance_id for u in permit.units),
                    "admission_selection", "Permit belongs to another instance.")
            # Permit identity is create-only: reissuing it must never replenish claims.
            directory = protected_path(self.host, self.runtime / "permits" / permit.id, "directory")
            require(not directory.exists(), "admission_permit_exists", "Validation permit already exists.")
            require(not directory.parent.exists() or len(list(directory.parent.iterdir())) < MAX_PERMITS,
                    "admission_retained", "Retained permits require proven-exit cleanup.")
            protected_directory(self.host, directory, 0o700)
            self._write(directory / "closure.json", evidence)
            self._write(directory / "permit.json", permit)

    def revoke_permit(self, permit_id):
        """Retain claims/identity so revocation can never replenish an allowance."""
        with self._lock():
            path = protected_path(self.host, self.runtime / "permits" / contract_uuid(permit_id) / "permit.json", "file")
            if path.exists():
                path.unlink()
                fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)

    def _selection(self):
        selection = read_owned(self.host, self.durable / "selection.json", DeploymentSelection.parse)
        resolution = read_owned(self.host, self.durable / "resolution.json", RuntimeResolution.parse)
        self._matching(selection, resolution)
        return selection, resolution

    def commit(self):
        """Publish only after the lifecycle owner durably records successful completion."""
        with self._lock():
            selection, _ = self._selection()
            self._completed(selection)
            self._write(self.durable / "selection.json", replace(selection, admission=replace(
                selection.admission, disposition=AdmissionDisposition.COMMITTED)))

    def _completed(self, selection):
        operations = self._operations()
        selected = operations.get(selection.admission.operation_id)
        require(selected and selected["outcome"] in ("succeeded", "reconciled") and
                all(a["outcome"] in TERMINAL_OUTCOMES for a in operations.values()),
                "admission_journal", "Completion is unproven or a transition remains unresolved.")

    def _validation(self, selection, permit):
        operations = self._operations()
        attempt = operations.get(permit.operation_id)
        require(selection.admission.disposition is AdmissionDisposition.PENDING and
                selection.admission.operation_id == permit.operation_id and
                selection.selected_resolution_id == permit.resolution_id and
                permit.boot_id == boot_id(self.host) and executor_alive(self.host, permit.executor) and
                attempt and attempt["outcome"] == "running" and attempt["phase"] in VALIDATION_PHASES and
                all(key == permit.operation_id or a["outcome"] in TERMINAL_OUTCOMES
                    for key, a in operations.items()),
                "admission_permit_invalid", "Validation requires a live owner in the permitted journal phase.")

    def _authorize(self, invocation, permit_id):
        selection, resolution = self._selection()
        self.require_cleanup_clear(selection.admission.operation_id if permit_id is not None else None)
        unit = invocation.unit
        require(invocation.boot_id == boot_id(self.host) and unit.instance_id == self.instance_id and
                invocation.resolution_id == resolution.id and unit.role in resolution.roles and
                unit.role not in selection.maintenance_holds,
                "admission_denied", "Start identity is stale, unselected or held.")
        evidence = read_owned(self.host, self.runtime / "boot.json", ClosureEvidence.parse)
        require(evidence.boot_id == invocation.boot_id and evidence.stage is ClosureStage.APPLICATION_CLOSED and
                evidence.candidate_resolution_id == resolution.id and unit in evidence.units and
                set(resolution.endpoints) <= set(evidence.endpoints) and
                set(resolution.roles) <= {u.role for u in evidence.units
                                         if u.instance_id == self.instance_id},
                "admission_boot_required", "Whole-instance boot preparation is missing or stale.")
        if selection.admission.disposition is AdmissionDisposition.COMMITTED:
            require(permit_id is None and unit.role in selection.enabled_roles,
                    "admission_denied", "Committed starts must be enabled.")
            self._completed(selection)
        else:
            directory = self.runtime / "permits" / contract_uuid(permit_id)
            permit = read_owned(self.host, directory / "permit.json", ValidationPermit.parse)
            closure = read_owned(self.host, directory / "closure.json", ClosureEvidence.parse)
            require(permit.id == permit_id and unit in permit.units, "admission_denied", "Unit has no validation allowance.")
            permit.require_closure(closure, resolution)
            self._validation(selection, permit)
        return resolution

    def admit(self, invocation, *, permit_id=None):
        invocation = HelperInvocation.parse(record_document(invocation))
        with self._lock(shared=True), self._lock("unit-" + invocation.unit.role):
            resolution = self._authorize(invocation, permit_id)
            if permit_id is not None:
                claim = protected_path(self.host, self.runtime / "permits" / permit_id /
                                       (invocation.unit.role + ".claim"), "file")
                if claim.exists():
                    require(claim.read_text() == invocation.process.invocation_id,
                            "admission_allowance_used", "Validation allowance belongs to an earlier invocation.")
                else:
                    atomic_write(claim, invocation.process.invocation_id, mode=0o600)
            receipt = self._receipt(invocation)
            if receipt.exists():
                require(read_owned(self.host, receipt, HelperInvocation.parse) == invocation,
                        "admission_invocation", "Invocation identity cannot change between helper phases.")
            else:
                require(not receipt.parent.exists() or len(list(receipt.parent.iterdir())) < MAX_ADMITTED_INVOCATIONS,
                        "admission_retained", "Retained invocations require proven-exit cleanup.")
                self._write(receipt, invocation)
            return resolution

    def _receipt(self, invocation):
        return self.runtime / "admitted" / invocation.unit.role / (invocation.process.invocation_id + ".json")

    def bind_workload(self, invocation, process, *, permit_id=None):
        """Transfer the admitted helper receipt to its observed systemd MainPID."""
        require(process.invocation_id == invocation.process.invocation_id,
                "admission_invocation", "Workload belongs to another service invocation.")
        with self.preparation(invocation, permit_id=permit_id):
            self._write(self._receipt(invocation), replace(invocation, process=process))

    @contextmanager
    def preparation(self, invocation, *, permit_id=None):
        """Recheck a granted invocation without claiming another start allowance."""
        invocation = HelperInvocation.parse(record_document(invocation))
        with self._lock(shared=True), self._lock("unit-" + invocation.unit.role):
            resolution = self._authorize(invocation, permit_id)
            require(read_owned(self.host, self._receipt(invocation), HelperInvocation.parse) == invocation,
                    "admission_invocation", "Preparation requires the admitted invocation.")
            if permit_id is not None:
                claim = protected_path(self.host, self.runtime / "permits" / permit_id /
                                       (invocation.unit.role + ".claim"), "file")
                require(claim.exists() and claim.read_text() == invocation.process.invocation_id,
                        "admission_allowance_used", "Preparation must use its already-claimed allowance.")
            yield resolution
