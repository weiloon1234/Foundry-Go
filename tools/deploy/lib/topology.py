"""Schema-1 supervision declarations and the single instance-unit map owner."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

from contracts import fields, read_json, require, unit_name


def validate_topology(value: dict, *, registered: bool = False) -> dict:
    """Validate roles; a registry projection also carries its canonical SHA-256."""
    fields(value, "schema roles sha256" if registered else "schema roles", "topology")
    require(type(value["schema"]) is int and value["schema"] == 1
            and isinstance(value["roles"], list), "invalid_topology", "Topology requires schema 1 and roles.")
    names = []
    for role in value["roles"]:
        fields(role, "name", "topology role")
        require(role["name"] in ("http", "scheduler", "worker") and role["name"] not in names,
                "invalid_topology", "Declare each supported role at most once.")
        names.append(role["name"])
    require("http" in names, "invalid_topology", "The HTTP role is required.")
    projection = {"schema": 1, "roles": [{"name": name} for name in names]}
    checksum = hashlib.sha256(json.dumps(projection, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if registered:
        require(value["sha256"] == checksum, "invalid_topology", "Topology checksum mismatch.")
    return {**projection, "sha256": checksum}


def load_topology(app_root: Path) -> dict:
    """Load the explicit application's deploy/topology.json; never infer roles."""
    return validate_topology(read_json(Path(app_root) / "deploy/topology.json"))


def instance_units(entry: dict) -> dict[str, str]:
    """Map every declared role to its canonical concrete systemd unit."""
    topology = validate_topology(entry["topology"], registered=True)
    return {role["name"]: unit_name(entry["name"], role["name"]) for role in topology["roles"]}


def selected_units(entry: dict, role="all", *, roles=None) -> dict[str, str]:
    units = instance_units(entry)
    if roles is not None:
        require(isinstance(roles, list) and all(isinstance(item, str) and item in units for item in roles)
                and len(roles) == len(set(roles)), "invalid_role", "Select declared topology roles.")
        return {name: unit for name, unit in units.items() if name in roles}
    require(isinstance(role, str) and (role == "all" or role in units),
            "invalid_role", "Select a declared topology role.")
    return units if role == "all" else {role: units[role]}
