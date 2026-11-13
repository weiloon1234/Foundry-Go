"""Lane A's narrow network handoff. Missing lane C support always fails closed.

Lane C supplies feature_allocate, feature_close and feature_open in listeners.py.
No caller may invent closure or treat an intended firewall change as observation.
"""
import importlib

from contracts import AllocatedEndpoint, ClosureEvidence, ClosureStage, DeployError, contract_digest, require


def capability():
    try:
        owner = importlib.import_module("listeners")
    except ModuleNotFoundError as error:
        if error.name != "listeners":
            raise
        owner = None
    require(owner is not None and all(callable(getattr(owner, name, None)) for name in
            ("feature_allocate", "feature_close", "feature_open")),
            "capability_unavailable", "Public activation requires the B24 lane C preview network integration release.")
    return owner


def allocate(host, entry, topology, profile, operation_id):
    """Return (tuple[AllocatedEndpoint], nginx projection SHA256), without effects."""
    endpoints, projection = capability().feature_allocate(host, entry, topology, profile, operation_id)
    from dataclasses import asdict
    endpoints = tuple(AllocatedEndpoint.parse(asdict(endpoint)) for endpoint in endpoints)
    return endpoints, contract_digest(projection)


def close(host, entry, transition, resolution, units):
    """Observe complete application closure for the exact transition's units/addresses."""
    from contracts import record_document
    evidence = capability().feature_close(host, entry, transition, resolution, units)
    evidence = ClosureEvidence.parse(record_document(evidence))
    require(evidence.stage is ClosureStage.APPLICATION_CLOSED and evidence.operation_id == transition.operation_id
            and evidence.candidate_resolution_id == resolution.id
            and set(transition.affected_units) <= set(evidence.units)
            and set(resolution.endpoints) <= set(evidence.endpoints),
            "admission_closed_required", "Network owner did not prove complete closure for this transition.")
    return evidence


def open_public(host, entry, resolution, evidence):
    require(capability().feature_open(host, entry, resolution, evidence) is True,
            "public_opening_unverified", "Network owner did not verify public opening for the selected resolution.")
