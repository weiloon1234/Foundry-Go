"""Durable enablement evidence for copies absent from a retained release."""
import admission
from contracts import UnitIdentity, atomic_json, require, unit_selection_identities, unit_selection_names


def removed_enablement(host, entry):
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    path = store.durable / "removed-enablement.json"
    value = admission.read_owned(host, path, lambda value: value) if path.exists() else {}
    require(isinstance(value, dict) and all(type(enabled) is bool for enabled in value.values()),
            "service_enablement", "Invalid removed-copy enablement evidence.")
    # Historical releases can refer to more copies than one placement permits.
    # read_owned/atomic_json retain the shared document-size bound.
    return {unit_selection_identities(entry["profile"]["instance_id"], (key,))[0]: enabled
            for key, enabled in value.items()}


def record_removed_copy_enablement(host, entry, identity: UnitIdentity, enabled: bool) -> None:
    """Persist one proven-stopped copy before removing its unit; retain across rollback releases.

    The lifecycle owner calls under its host/instance locks after exit and closure
    proofs, before disabling the removed unit. Entries are not consumed on restoration: older retained releases
    can still refer to them. A subsequent removal replaces that copy's evidence.
    """
    require(isinstance(identity, UnitIdentity) and type(enabled) is bool
            and identity.instance_id == entry["profile"]["instance_id"],
            "service_enablement", "Invalid removed-copy enablement evidence.")
    merged = {**removed_enablement(host, entry), identity: enabled}
    store = admission.AdmissionStore(host, entry["profile"]["instance_id"])
    admission.protected_directory(host, store.durable, 0o700)
    atomic_json(store.durable / "removed-enablement.json",
                {unit_selection_names((key,))[0]: merged[key] for key in sorted(merged)}, mode=0o600)


def restoration_plan(host, entry, previous, inventory):
    """Explain absent copies, including disabled selections with missing evidence."""
    evidence = removed_enablement(host, entry)
    return [{"role": identity.role, "ordinal": identity.ordinal,
             "enabled": evidence.get(identity, False),
             "evidence": "recorded" if identity in evidence else "missing"}
            for identity in inventory.units if identity not in previous.units]
