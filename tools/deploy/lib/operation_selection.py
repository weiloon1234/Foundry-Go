"""Client and host validation for the public ROLE/COPY operation selector."""
from contracts import MAX_ROLE_COPIES, UnitIdentity, require
from topology import selected_units


def validate(action, role, ordinal):
    if ordinal is None:
        return
    require(action in ("stop", "restart", "logs"), "invalid_copy", "COPY is only supported for stop, restart and logs.")
    require(isinstance(role, str) and role != "all" and bool(role),
            "invalid_copy", "COPY requires an explicit ROLE.")
    require(type(ordinal) is int and 1 <= ordinal <= MAX_ROLE_COPIES,
            "invalid_copy", f"COPY must be an integer from 1 to {MAX_ROLE_COPIES}.")


def units(entry, role="all", ordinal=None):
    selected = selected_units(entry, role)
    if ordinal is None:
        return selected
    validate("logs", role, ordinal)
    identity = UnitIdentity(entry["profile"]["instance_id"], role, ordinal)
    require(identity in selected, "invalid_copy", "COPY must name a placed ordinal of ROLE.")
    return {identity: selected[identity]}
