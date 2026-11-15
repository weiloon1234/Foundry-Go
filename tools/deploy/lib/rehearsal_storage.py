"""Reduced-fidelity storage is issued only inside the guarded disposable run."""
import json
import os
from pathlib import Path
import re

from contracts import check_path, read_json, require

RUN_ID = "FOUNDRY_REHEARSAL_RUN_ID"
MODE = "rehearsal_runtime"
CONTEXT = Path("/var/lib/foundry-rehearsal/context.json")


def _context(entry):
    token = os.environ.get(RUN_ID, "")
    if not token:
        return None
    require(re.fullmatch(r"[a-f0-9]{32}", token) is not None and Path("/.dockerenv").is_file(),
            "rehearsal_storage_identity", "Storage rehearsal requires a disposable run identity.")
    context = read_json(check_path(CONTEXT, "file"))
    profile = entry["profile"]
    require(context.get("run_id") == token and context.get("profile") == profile
            and profile.get("runtime_environment") == "testing"
            and profile.get("environment", "").startswith("acceptance")
            and profile.get("domain", "").endswith(".acceptance.invalid"),
            "rehearsal_storage_identity", "Storage rehearsal identity differs from the disposable acceptance profile.")
    return context


def run_identity(entry):
    context = _context(entry)
    return context["run_id"] if context else None


def environment(entry):
    token = run_identity(entry)
    return {RUN_ID: token} if token else {}


def unit_environment(entry):
    return "".join("Environment=" + key + "=" + value + "\n" for key, value in environment(entry).items())


def reject_marker(value):
    """Archive and generic configuration paths never issue rehearsal markers."""
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except ValueError:
            return
        if isinstance(parsed, (dict, list)):
            reject_marker(parsed)
    elif isinstance(value, dict):
        for key, item in value.items():
            require(not (isinstance(key, str) and key.lower().split("__")[-1] == "conditional_write_mode" and item),
                    "rehearsal_storage_configuration", "Reduced-fidelity storage markers are generated only by rehearse.")
            reject_marker(item)
    elif isinstance(value, list):
        for item in value:
            reject_marker(item)


def descriptor(entry):
    """Issue a descriptor block from private deploy-owned rehearsal state only."""
    context = _context(entry)
    if context is None:
        return None
    disks = context.get("storage_runtime_caps", [])
    require(isinstance(disks, list) and len(disks) <= 64, "rehearsal_storage_configuration", "Invalid rehearsal storage grants.")
    names, caps = set(), []
    for disk in disks:
        require(isinstance(disk, dict) and set(disk) == {"disk", "runtime_cap_bytes"}
                and isinstance(disk["disk"], str) and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{0,127}", disk["disk"])
                and disk["disk"] not in names and type(disk["runtime_cap_bytes"]) is int
                and 5 << 20 <= disk["runtime_cap_bytes"] <= 64 << 20,
                "rehearsal_storage_configuration", "Invalid rehearsal storage grant.")
        names.add(disk["disk"])
        caps.append(dict(disk))
    return {"run_id": context["run_id"], "disks": caps} if caps else None


def collect(host, units):
    """Select only typed public fields from bounded journal JSON, never raw logs."""
    result = {}
    for unit in units.values():
        output = host.run(["journalctl", "-u", unit, "-n", "4096", "--output=json", "--no-pager"],
                          timeout=10, check=False)
        for line in output.stdout.splitlines():
            if len(line) > 65536:
                continue
            try:
                envelope = json.loads(line)
                record = json.loads(envelope.get("MESSAGE", ""))
            except (ValueError, TypeError, AttributeError):
                continue
            if not isinstance(record, dict):
                continue
            event, disk, cap = record.get("event"), record.get("disk"), record.get("runtime_cap_bytes")
            if (event not in ("foundry_rehearsal_storage", "foundry_rehearsal_storage_refusal")
                    or not isinstance(disk, str) or re.fullmatch(r"[a-z0-9][a-z0-9_.-]{0,127}", disk) is None
                    or type(cap) is not int or not 0 < cap <= 64 << 20):
                continue
            row = result.setdefault(disk, {"disk": disk, "runtime_cap_bytes": cap,
                                           "exceeding_policy_maxima": [], "runtime_refusals_observed": 0, "policy_observation": "pending"})
            if event == "foundry_rehearsal_storage":
                row["policy_observation"] = "observed"
                maximum = record.get("max_object_bytes")
                if type(maximum) is int and 0 < maximum <= 1 << 50:
                    row["max_object_bytes"] = maximum
                policies = record.get("exceeding_policy_maxima")
                if isinstance(policies, list) and len(policies) <= 4096:
                    for policy in policies:
                        if not isinstance(policy, dict):
                            continue
                        name, maximum = policy.get("name"), policy.get("max_bytes")
                        if (isinstance(name, str) and re.fullmatch(r"[a-zA-Z0-9_.:-]{1,256}", name)
                                and type(maximum) is int and cap < maximum <= 1 << 50):
                            item = {"name": name, "max_bytes": maximum}
                            if item not in row["exceeding_policy_maxima"]:
                                row["exceeding_policy_maxima"].append(item)
            else:
                # Each refusal emits one record; cumulative process counters may
                # reset on restart. Count events across all selected units.
                row["runtime_refusals_observed"] += 1
    return [result[name] for name in sorted(result)]
