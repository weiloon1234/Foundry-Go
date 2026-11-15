"""Optional real-systemd operations assertions for the merged numbered-copy lane."""
import json
from pathlib import Path
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from acceptance_support import LAUNCHER, action, guard, request, run
import backup
import feature_lifecycle
import feature_maintenance
import report
import services


def assert_copies_operations(host, project, profile, *, role="relay", backup_id=None):
    """Call after deploying a disposable fixture with two enabled independent copies.

    The selected role must use maintenance: stop and have no enabled dependents.
    This exercises copy-only and whole-role logs, stop/restart, persistent holds,
    exact up enablement and per-copy report/state coverage. If backup_id names a
    completed backup of this placement, it also runs a restore drill and proves
    every copy's state sentinel survives. It leaves the role enabled and running.
    No containers are created. The caller owns fixture bootstrap and cleanup.
    """
    guard()
    _, entry = host.require_identity(project, profile)
    _, _, inventory, _ = feature_lifecycle.load(host, entry)
    first, second, *rest = inventory.copies(role)
    assert not rest, "Operations scenario requires exactly two selected copies"
    assert inventory.roles[role].common.maintenance == "stop"
    enabled = set(services.enabled_units(host, inventory.units))
    assert {first, second} <= enabled
    assert not any(inventory.prerequisites({key}) & {first, second} for key in enabled - {first, second})
    import release
    original_pid = release.service(host, inventory.units[first])["MainPID"]
    result = action("stop", request(project, profile, role=role, copy=2))
    assert result["status"] == "degraded"
    assert second not in services.enabled_units(host, inventory.units)
    assert release.service(host, inventory.units[first])["MainPID"] == original_pid
    for ordinal in (None, 2):
        payload = request(project, profile, role=role, lines=10, **({"copy": ordinal} if ordinal else {}))
        logs = json.loads(run([LAUNCHER, "logs"], input=json.dumps(payload)).stdout)
        assert isinstance(logs["logs"], str)
    action("down", request(project, profile))
    for identity in (first, second):
        assert host.paths.p(feature_maintenance.marker_path(identity)).exists()
    down = report.collect_report(host)
    instance = next(item for group in down["projects"] for item in group["instances"] if item["instance_id"] == profile["instance_id"])
    rows = instance["services"][role]["copies"]
    assert {row["ordinal"] for row in rows} == {1, 2}
    assert all(row["held"] and row["readiness"]["status"] == "held" for row in rows)
    assert rows[0]["persistent_state_paths"] != rows[1]["persistent_state_paths"]
    assert all(key in row for row in rows for key in ("admission", "process", "listeners", "confinement", "shared_directories"))
    action("up", request(project, profile))
    enabled = set(services.enabled_units(host, inventory.units))
    assert first in enabled and second not in enabled
    action("restart", request(project, profile, role=role, copy=2))
    assert {first, second} <= set(services.enabled_units(host, inventory.units))
    coverage = backup.service_coverage(inventory)
    assert {row["ordinal"] for row in coverage["service_state"] if row["role"] == role} == {1, 2}
    if backup_id is not None:
        sentinels = []
        token = uuid.uuid4().hex
        try:
            for identity in (first, second):
                state = host.paths.p(services.ServicePaths.for_unit(identity, inventory.resolution.id).state)
                assert state.is_dir()
                sentinel = state / ("ops-drill-" + token)
                with sentinel.open("x") as output:
                    output.write(token + str(identity.ordinal))
                sentinels.append((sentinel, token + str(identity.ordinal)))
            drill = action("restore-drill", request(project, profile, backup_id=backup_id))
            assert all(path.read_text() == value for path, value in sentinels)
            states = drill.get("restore_drill", drill).get("verification", {}).get("service_state", [])
            assert {row["ordinal"] for row in states if row["role"] == role} == {1, 2}
            assert all(row["restored"] is False for row in states)
        finally:
            for path, _ in sentinels:
                path.unlink(missing_ok=True)
    return {"role": role, "copies": [1, 2], "coverage": coverage}
