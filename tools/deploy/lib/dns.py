"""Owned Cloudflare records and durable, single-attempt provider mutations.

Callers hold the instance lock. Provider observations never grant public admission.
"""
from dataclasses import asdict, dataclass
from contextlib import nullcontext
from enum import Enum
from http.client import HTTPException
import ipaddress
import json
import re
import signal
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

import admission
import certificates
from contracts import (DeployError, atomic_json, canonical_digest, contract_int, contract_list,
                       contract_object, contract_uuid, dns_hostname, public_address, require)
from host import OperationTimeout, wall_clock_budget

MAX_RECORDS = 128
MAX_RESPONSE = 256 * 1024
APPLY_SECONDS = 300
API = "https://api.cloudflare.com/client/v4"


class PublicAdmission(str, Enum):
    PENDING = "pending"
    COMMITTED = "committed"


def provider_id(value):
    require(isinstance(value, str) and re.fullmatch(r"[a-f0-9]{32}", value),
            "dns_identity", "Invalid Cloudflare zone or record ID.")
    return value


@dataclass(frozen=True)
class Record:
    ownership_id: str
    address: str
    ttl: int

    @classmethod
    def parse(cls, value):
        contract_object(value, ("ownership_id", "address", "ttl"))
        ttl = contract_int(value["ttl"], 1, 86400)
        require(ttl == 1 or ttl >= 60, "dns_ttl", "DNS TTL must be automatic (1) or 60–86400 seconds.")
        return cls(contract_uuid(value["ownership_id"]), public_address(value["address"]), ttl)


@dataclass(frozen=True)
class Value:
    name: str
    type: str
    content: str
    ttl: int
    proxied: bool
    comment: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("name", "type", "content", "ttl", "proxied", "comment"))
        address = public_address(value["content"])
        require(value["type"] == ("A" if ipaddress.ip_address(address).version == 4 else "AAAA")
                and type(value["proxied"]) is bool and isinstance(value["comment"], str)
                and len(value["comment"]) <= 256, "dns_record", "Invalid provider record state.")
        return cls(dns_hostname(value["name"]), value["type"], address,
                   contract_int(value["ttl"], 1, 86400), value["proxied"], value["comment"])


@dataclass(frozen=True)
class ProviderState:
    record_id: str
    value: Value
    version: str | None
    etag: str | None
    metadata_sha256: str

    @classmethod
    def parse(cls, value):
        from contracts import contract_digest
        contract_object(value, ("record_id", "value", "version", "etag", "metadata_sha256"))
        for key in ("version", "etag"):
            require(value[key] is None or isinstance(value[key], str) and len(value[key]) <= 256
                    and all(ord(c) >= 32 for c in value[key]), "dns_record", "Invalid provider version.")
        return cls(provider_id(value["record_id"]), Value.parse(value["value"]), value["version"], value["etag"],
                   contract_digest(value["metadata_sha256"]))


@dataclass(frozen=True)
class Change:
    zone_id: str
    ownership_id: str
    instance_id: str
    create_operation_id: str
    prior: ProviderState | None
    desired: Value | None

    @classmethod
    def parse(cls, value):
        contract_object(value, ("zone_id", "ownership_id", "instance_id", "create_operation_id", "prior", "desired"))
        result = cls(provider_id(value["zone_id"]), contract_uuid(value["ownership_id"]),
                     contract_uuid(value["instance_id"]), contract_uuid(value["create_operation_id"]),
                     ProviderState.parse(value["prior"]) if value["prior"] is not None else None,
                     Value.parse(value["desired"]) if value["desired"] is not None else None)
        require(result.prior is not None or result.desired is not None, "dns_intent", "Empty DNS mutation.")
        require(all(v.comment == result.comment for v in (result.prior.value if result.prior else None, result.desired) if v),
                "dns_ownership", "Provider record differs from its durable creation identity.")
        require(result.desired is None or result.desired.proxied is False,
                "dns_proxy", "Public TCP records must be DNS-only.")
        require(result.prior is None or result.desired is None or
                (result.prior.value.name, result.prior.value.type) == (result.desired.name, result.desired.type),
                "dns_identity", "A record ownership identity cannot change hostname or address family.")
        return result

    @property
    def comment(self):
        return f"foundry:{self.instance_id}:{self.ownership_id}:{self.create_operation_id}"


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Cloudflare:
    """Fixed HTTPS origin, no redirects/proxies/retries, bounded I/O and safe faults."""
    def __init__(self, token):
        certificates._validate_token(token)
        self._token = token
        self._opener = build_opener(NoRedirect(), ProxyHandler({}))

    def request(self, method, zone, suffix="", body=None):
        url = API + "/zones/" + provider_id(zone) + "/dns_records" + suffix
        data = json.dumps(body, separators=(",", ":")).encode() if body is not None else None
        require(data is None or len(data) <= 4096, "dns_request_bound", "DNS request exceeds its bound.")
        request = Request(url, data=data, method=method,
                          headers={"Authorization": "Bearer " + self._token, "Content-Type": "application/json"})
        try:
            remaining = signal.getitimer(signal.ITIMER_REAL)[0]
            with nullcontext() if 0 < remaining <= 15 else wall_clock_budget(15):
                with self._opener.open(request, timeout=10) as response:
                    raw = response.read(MAX_RESPONSE + 1)
                    require(len(raw) <= MAX_RESPONSE, "dns_response_bound", "DNS response exceeds its bound.")
                    value = json.loads(raw)
                    require(isinstance(value, dict) and value.get("success") is True,
                            "dns_provider_error", "Cloudflare rejected the DNS request.")
                    return value, response.headers.get("ETag")
        except HTTPError as error:
            code = error.code
            error.close()
            if code == 404 and method == "GET" and suffix.startswith("/"):
                return None, None
            raise DeployError("dns_rate_limited" if code == 429 else "dns_provider_error",
                              "Cloudflare request failed; no automatic retry was made.") from None
        except (URLError, OSError, ValueError, HTTPException) as error:
            from host import OperationTimeout
            if isinstance(error, OperationTimeout) and 0 < remaining <= 15:
                raise
            raise DeployError("dns_provider_unavailable", "Cloudflare response was unavailable or invalid; reconcile any pending mutation.") from None

    def _state(self, value, etag=None):
        require(isinstance(value, dict), "dns_record", "Invalid provider record.")
        fields = {key: value.get(key) for key in ("name", "type", "content", "ttl", "proxied")}
        fields["comment"] = value.get("comment") or ""
        # Preserve metadata changes without publishing arbitrary provider fields.
        metadata = {k: v for k, v in value.items() if k not in {*fields, "id", "modified_on", "comment_modified_on", "tags_modified_on"}}
        return ProviderState.parse({"record_id": value.get("id"), "value": fields,
                                    "version": value.get("modified_on"), "etag": etag,
                                    "metadata_sha256": canonical_digest(metadata)})

    def records(self, zone, name):
        value, _ = self.request("GET", zone, "?" + urlencode({"name": dns_hostname(name), "per_page": MAX_RECORDS}))
        records = value.get("result")
        info = value.get("result_info")
        require(isinstance(records, list) and len(records) <= MAX_RECORDS and isinstance(info, dict)
                and type(info.get("total_pages")) is int and info["total_pages"] <= 1,
                "dns_record_bound", "DNS record inventory is incomplete or exceeds its bound.")
        # Refuse other record types at the exact name before projecting address records.
        require(all(isinstance(r, dict) and r.get("name") == name and r.get("type") in ("A", "AAAA") for r in records),
                "dns_foreign_record", "The hostname contains an unrelated provider record.")
        return tuple(self._state(r) for r in records)

    def get(self, zone, record_id):
        value, etag = self.request("GET", zone, "/" + provider_id(record_id))
        result = None if value is None else self._state(value.get("result"), etag)
        require(result is None or result.record_id == record_id, "dns_identity", "Provider returned a different record ID.")
        return result

    def write(self, change):
        if change.prior is None:
            self.request("POST", change.zone_id, body=asdict(change.desired))
        elif change.desired is None:
            self.request("DELETE", change.zone_id, "/" + change.prior.record_id)
        else:
            self.request("PATCH", change.zone_id, "/" + change.prior.record_id, asdict(change.desired))


def provider(host, entry, zone):
    require(entry.get("recovered_from", {}).get("mode") != "rehearsal", "dns_rehearsal", "Rehearsal cannot access managed DNS.")
    return Cloudflare(certificates.dns_token(host, entry, zone))


def state_path(host, entry):
    return admission.protected_path(host, host.paths.instance_config(entry["name"]) / "managed-dns.json", "file")


def provider_applied(item):
    return len(item["results"]) == len(item["changes"]) and set(item["results"].values()) <= {"applied"}


def parse_state(value, instance_id):
    contract_object(value, ("schema", "instance_id", "owned", "operations"))
    require(value["schema"] == 1 and value["instance_id"] == instance_id
            and isinstance(value["owned"], dict) and len(value["owned"]) <= MAX_RECORDS
            and isinstance(value["operations"], dict) and len(value["operations"]) <= MAX_RECORDS,
            "dns_state", "Invalid managed DNS ownership state.")
    for key, raw in value["owned"].items():
        change = Change.parse(raw)
        require(key == change.ownership_id and change.instance_id == instance_id and change.prior is not None,
                "dns_state", "Invalid owned record identity.")
    for key, item in value["operations"].items():
        contract_uuid(key)
        contract_object(item, ("changes", "results"), ("public_admission",))
        require("public_admission" not in item or item["public_admission"] in {s.value for s in PublicAdmission},
                "dns_state", "Invalid DNS public admission phase.")
        changes = contract_list(item["changes"], Change.parse, maximum=MAX_RECORDS)
        require(all(c.instance_id == instance_id for c in changes) and isinstance(item["results"], dict)
                and set(item["results"]) <= {c.ownership_id for c in changes}
                and all(s in ("pending", "applied", "prior", "conflict") for s in item["results"].values()),
                "dns_state", "Invalid DNS operation identity or result.")
        require(item.get("public_admission") != PublicAdmission.COMMITTED.value or provider_applied(item),
                "dns_state", "Committed DNS admission requires complete provider outcomes.")
    return value


def load(host, entry):
    path = state_path(host, entry)
    if not path.exists():
        return {"schema": 1, "instance_id": entry["profile"]["instance_id"], "owned": {}, "operations": {}}
    return admission.read_owned(host, path, lambda v: parse_state(v, entry["profile"]["instance_id"]))


def save(host, entry, state):
    parse_state(state, entry["profile"]["instance_id"])
    atomic_json(state_path(host, entry), state, mode=0o600)


def _desired(binding, record, comment):
    return Value(binding.hostname, "A" if ipaddress.ip_address(record.address).version == 4 else "AAAA",
                 record.address, record.ttl, False, comment)


def _applied(change, current):
    if change.desired is None:
        return current is None
    return (current is not None and current.value == change.desired
            and (change.prior is None or current.record_id == change.prior.record_id
                 and current.metadata_sha256 == change.prior.metadata_sha256))


def _outcome(change, current):
    if _applied(change, current):
        return "applied"
    if change.prior is not None and current == change.prior:
        return "prior"
    return "conflict" if change.prior is not None or current is not None else "pending"


def _read_result(client, change):
    if change.prior:
        return client.get(change.zone_id, change.prior.record_id)
    records = client.records(change.zone_id, change.desired.name)
    matches = [r for r in records if r.value.comment == change.comment]
    require(len(matches) <= 1, "dns_ownership", "Creation identity matched multiple records; operator review is required.")
    return matches[0] if matches else None


def reconcile(host, entry, state, operation_id, *, clients=None):
    """Read only. An absent unknown create never authorizes another POST."""
    item = state["operations"][operation_id]
    clients = {} if clients is None else clients
    for raw in item["changes"]:
        change = Change.parse(raw)
        key = change.ownership_id
        if item["results"].get(key) not in ("pending", "prior"):
            continue
        client = clients.setdefault(change.zone_id, None)
        if client is None:
            client = clients[change.zone_id] = provider(host, entry, change.zone_id)
        current = _read_result(client, change)
        item["results"][key] = _outcome(change, current)
        if item["results"][key] == "applied":
            if current is None:
                state["owned"].pop(key, None)
            else:
                state["owned"][key] = asdict(Change(change.zone_id, key, change.instance_id,
                                                   change.create_operation_id, current, current.value))
        save(host, entry, state)
    return item["results"]


def plan(host, entry, profile, operation_id):
    operation_id = contract_uuid(operation_id)
    require(profile.instance_id == entry["profile"]["instance_id"], "dns_identity", "DNS profile belongs to another instance.")
    state = load(host, entry)
    for previous, item in state["operations"].items():
        if "pending" in item["results"].values():
            reconcile(host, entry, state, previous)
        require(not {"pending", "conflict"}.intersection(item["results"].values()),
                "dns_reconciliation", "A DNS mutation is unresolved; inspect its operation and record IDs before planning.")
    desired = {}
    for _, binding in profile.listeners:
        for record in binding.records:
            previous = state["owned"].get(record.ownership_id)
            creation = Change.parse(previous).create_operation_id if previous else operation_id
            change = Change(binding.zone_id, record.ownership_id, profile.instance_id, creation, None, None)
            change = Change(change.zone_id, change.ownership_id, change.instance_id, creation, None,
                            _desired(binding, record, change.comment))
            require(record.ownership_id not in desired or desired[record.ownership_id] == change,
                    "dns_ownership", "A DNS ownership ID cannot describe different records.")
            desired[record.ownership_id] = change
    changes, clients, names = [], {}, {}
    for key in sorted(desired.keys() | state["owned"].keys()):
        owned = Change.parse(state["owned"][key]) if key in state["owned"] else None
        target = desired.get(key)
        zone = owned.zone_id if owned else target.zone_id
        require(target is None or target.zone_id == zone, "dns_zone_mismatch", "An ownership ID cannot move zones.")
        if zone not in clients:
            clients[zone] = provider(host, entry, zone)
        client = clients[zone]
        before = client.get(zone, owned.prior.record_id) if owned else None
        require(owned is None or before is not None and before.value.comment == owned.comment,
                "dns_ownership", "The owned provider record is missing or its ownership evidence changed.")
        change = Change(zone, key, profile.instance_id, owned.create_operation_id if owned else operation_id,
                        before, target.desired if target else None)
        Change.parse(asdict(change))
        changes.append(change)
        name = (change.desired or change.prior.value).name
        names[(zone, name)] = None
    require(len(changes) <= MAX_RECORDS, "dns_record_bound", "Managed record count exceeds its bound.")
    for zone, name in names:
        observed = clients[zone].records(zone, name)
        allowed = {c.prior.record_id for c in changes if c.zone_id == zone and c.prior and c.prior.value.name == name}
        require(all(r.record_id in allowed for r in observed), "dns_foreign_record",
                "The hostname contains an unowned record; Foundry never adopts existing DNS records.")
    return [asdict(c) for c in changes]


def verify(host, entry, changes):
    """Recheck the complete plan before the first write, including foreign records."""
    parsed = contract_list(changes, Change.parse, maximum=MAX_RECORDS)
    state = load(host, entry)
    clients = {}
    for change in parsed:
        require(change.instance_id == entry["profile"]["instance_id"], "dns_ownership", "DNS plan belongs to another instance.")
        owned = Change.parse(state["owned"][change.ownership_id]) if change.ownership_id in state["owned"] else None
        require((change.prior is None and owned is None) or
                (change.prior is not None and owned is not None and change.zone_id == owned.zone_id
                 and change.prior.record_id == owned.prior.record_id and change.comment == owned.comment),
                "dns_ownership", "Plan record IDs differ from durable ownership.")
        if change.zone_id not in clients:
            clients[change.zone_id] = provider(host, entry, change.zone_id)
        client = clients[change.zone_id]
        current = client.get(change.zone_id, change.prior.record_id) if change.prior else None
        require(current == change.prior, "dns_drift", "Provider state changed after review; create a fresh plan.")
    for zone, name in {(c.zone_id, (c.desired or c.prior.value).name) for c in parsed}:
        allowed = {c.prior.record_id for c in parsed if c.zone_id == zone and c.prior and c.prior.value.name == name}
        require(all(r.record_id in allowed for r in clients[zone].records(zone, name)),
                "dns_foreign_record", "An unowned record appeared after review.")
    return clients


def apply(host, entry, changes, operation_id, *, track_public_admission=False):
    """Bound the entire provider phase independently of service readiness."""
    remaining = signal.getitimer(signal.ITIMER_REAL)[0]
    enclosing = 0 < remaining <= APPLY_SECONDS
    try:
        with nullcontext() if enclosing else wall_clock_budget(APPLY_SECONDS):
            return _apply(host, entry, changes, operation_id, track_public_admission=track_public_admission)
    except OperationTimeout:
        if enclosing:
            raise
        raise DeployError("dns_budget_exhausted", "DNS application exceeded its budget; reconcile its operation and record IDs. No write was retried.") from None


def _apply(host, entry, changes, operation_id, *, track_public_admission):
    operation_id = contract_uuid(operation_id)
    if not changes:
        return {}
    state = load(host, entry)
    if operation_id in state["operations"]:
        require(state["operations"][operation_id]["changes"] == changes, "dns_intent", "DNS operation intent is immutable.")
        if track_public_admission and "public_admission" not in state["operations"][operation_id]:
            state["operations"][operation_id]["public_admission"] = PublicAdmission.PENDING.value
            save(host, entry, state)
        results = reconcile(host, entry, state, operation_id)
        require(len(results) == len(changes) and set(results.values()) <= {"applied"}, "dns_reconciliation",
                "DNS operation is unresolved. Exact prior state requires a newly reviewed operation; no write was retried.")
        return results
    require(not any({"pending", "conflict"}.intersection(i["results"].values()) for i in state["operations"].values()),
            "dns_reconciliation", "Resolve the previous DNS operation before another mutation.")
    require(not any(i.get("public_admission") == PublicAdmission.PENDING.value for i in state["operations"].values()),
            "dns_public_admission_pending", "Resolve the previous deployment admission before replacing its DNS intent.")
    clients = verify(host, entry, changes)
    # Retain only unresolved intents plus the latest completed operation; the
    # creation UUID stays with each owned record independently of journal retention.
    state["operations"] = {operation_id: {"changes": changes, "results": {}}}
    if track_public_admission:
        state["operations"][operation_id]["public_admission"] = PublicAdmission.PENDING.value
    save(host, entry, state)
    item = state["operations"][operation_id]
    for raw in changes:
        change = Change.parse(raw)
        client = clients[change.zone_id]
        # Cloudflare exposes no documented conditional DNS update. Narrow the
        # comparison/write interval but do not claim it excludes concurrent edits.
        if change.prior:
            require(client.get(change.zone_id, change.prior.record_id) == change.prior,
                    "dns_drift", "Provider record changed immediately before mutation.")
        item["results"][change.ownership_id] = "pending"
        save(host, entry, state)
        try:
            if change.prior is None or change.desired != change.prior.value:
                client.write(change)
        except DeployError:
            # A transport or provider failure does not establish nonpublication.
            # Reconcile once by identity; never repeat the mutation.
            try:
                reconcile(host, entry, state, operation_id, clients=clients)
            except DeployError:
                pass
            if item["results"].get(change.ownership_id) != "applied":
                raise DeployError("dns_reconciliation", "DNS outcome is unresolved; inspect the operation UUID and owned record IDs. No write was retried.") from None
        reconcile(host, entry, state, operation_id, clients=clients)
        require(item["results"].get(change.ownership_id) == "applied", "dns_reconciliation",
                "DNS read-back did not prove the requested state; public admission remains closed.")
    return item["results"]


def commit_public_admission(host, entry, operation_id):
    """Called by the lifecycle owner only after successful admission commit."""
    state = load(host, entry)
    item = state["operations"].get(contract_uuid(operation_id))
    if item is None or "public_admission" not in item:
        return
    require(provider_applied(item),
            "dns_reconciliation", "Public admission cannot complete with unresolved DNS writes.")
    item["public_admission"] = PublicAdmission.COMMITTED.value
    save(host, entry, state)


def report(host, entry, profile):
    state = load(host, entry)
    values = []
    for _, binding in profile.listeners:
        if binding.dns_mode != "managed":
            continue
        value = {"mode": "managed", "hostname": binding.hostname, "zone_id": binding.zone_id,
                 "proxied": False, "state": "verified", "records": [], "propagation": "unverified"}
        try:
            client = provider(host, entry, binding.zone_id)
            observed = client.records(binding.zone_id, binding.hostname)
            ids = set()
            for record in binding.records:
                raw = state["owned"].get(record.ownership_id)
                owned = Change.parse(raw) if raw else None
                current = client.get(binding.zone_id, owned.prior.record_id) if owned else None
                matched = owned is not None and current is not None and current.value == _desired(binding, record, owned.comment)
                if owned:
                    ids.add(owned.prior.record_id)
                value["records"].append({"ownership_id": record.ownership_id, "record_id": owned.prior.record_id if owned else None,
                                         "drift": not matched, "ownership": asdict(owned) if owned else None,
                                         "provider": asdict(current) if current and owned and current.value.comment == owned.comment else None})
                if not matched:
                    value["state"] = "drift"
            if any(r.record_id not in ids for r in observed):
                value["state"] = "foreign_record"
        except DeployError:
            value["state"] = "unavailable"
        values.append(value)
    selected = {r.ownership_id for _, binding in profile.listeners for r in binding.records}
    for key in sorted(state["owned"].keys() - selected):
        owned = Change.parse(state["owned"][key])
        value = {"mode": "managed", "state": "retained_record", "hostname": owned.prior.value.name,
                 "zone_id": owned.zone_id, "ownership_id": key, "record_id": owned.prior.record_id,
                 "ownership": asdict(owned), "provider": None, "provider_state": "unavailable"}
        try:
            current = provider(host, entry, owned.zone_id).get(owned.zone_id, owned.prior.record_id)
            value["provider_state"] = "absent" if current is None else "published" if current.value == owned.prior.value else "drift"
            if current is not None and current.value.comment == owned.comment:
                value["provider"] = asdict(current)
        except DeployError:
            pass
        values.append(value)
    for operation_id, item in state["operations"].items():
        admission_pending = item.get("public_admission") == PublicAdmission.PENDING.value
        if admission_pending or not provider_applied(item):
            observed = {}
            for raw in item["changes"]:
                change = Change.parse(raw)
                if change.ownership_id not in item["results"]:
                    observed[change.ownership_id] = "not_attempted"
                    continue
                try:
                    current = _read_result(provider(host, entry, change.zone_id), change)
                    observed[change.ownership_id] = _outcome(change, current)
                except DeployError:
                    observed[change.ownership_id] = "unavailable"
            values.append({"mode": "managed", "state": "public_admission_pending" if admission_pending else "reconciliation_required", "operation_id": operation_id,
                           "results": item["results"], "observed_results": observed, "changes": item["changes"]})
    return values


def recovery_changes(binding):
    changes = {}
    for listener in binding.listeners:
        for change in listener.managed_records:
            require(change.ownership_id not in changes or changes[change.ownership_id] == change,
                    "dns_recovery", "Shared recovery record intents differ.")
            changes[change.ownership_id] = change
    return [asdict(changes[key]) for key in sorted(changes)]


def recovery_review(binding, metadata, archived_state, token):
    """Compare caller-supplied recovery snapshots with archived custody and fresh DNS."""
    state = parse_state(archived_state, binding.source_instance_id)
    require(not any(not provider_applied(i) or i.get("public_admission") == PublicAdmission.PENDING.value
                    for i in state["operations"].values()),
            "dns_recovery", "Archived DNS has unresolved mutations or deployment admission; reconcile on the source before takeover.")
    client = Cloudflare(token)
    for raw in recovery_changes(binding):
        change = Change.parse(raw)
        owned = Change.parse(state["owned"][change.ownership_id]) if change.ownership_id in state["owned"] else None
        require(owned is not None and change.instance_id == binding.source_instance_id
                and change.zone_id == owned.zone_id == metadata.get("certificate", {}).get("zone_ref")
                and change.create_operation_id == owned.create_operation_id
                and change.prior.record_id == owned.prior.record_id
                and change.prior.value.comment == owned.comment,
                "dns_recovery", "Recovery record IDs or ownership differ from the archived creation receipt.")
        require(client.get(change.zone_id, change.prior.record_id) == change.prior,
                "dns_drift", "Recovery provider state differs from the reviewed prior binding.")
    parsed = [Change.parse(raw) for raw in recovery_changes(binding)]
    for zone, name in {(c.zone_id, c.prior.value.name) for c in parsed}:
        allowed = {c.prior.record_id for c in parsed if c.zone_id == zone and c.prior.value.name == name}
        require(all(r.record_id in allowed for r in client.records(zone, name)),
                "dns_foreign_record", "Recovery hostname contains an unrelated record.")


def recovery_apply(host, entry, intent, *, old_server_stopped, private_healthy):
    from contracts import RecoveryPublicBinding
    raw = intent.get("recovery_public_binding")
    if raw is None:
        return {}
    binding = RecoveryPublicBinding.parse(raw)
    changes = recovery_changes(binding)
    if not changes:
        return {}
    require(intent["mode"] == "takeover" and old_server_stopped and private_healthy
            and binding.operation_id == intent["operation_id"]
            and binding.target_host_id == entry["profile"]["host"]["host_id"],
            "dns_recovery_not_ready", "Managed cutover requires confirmed stop and private takeover health.")
    return apply(host, entry, changes, binding.operation_id, track_public_admission=True)
