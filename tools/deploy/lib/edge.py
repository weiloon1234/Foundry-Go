"""Versioned Cloudflare trust data and nginx rendering; no server-side fetching."""
from datetime import date
import hashlib
import ipaddress
from pathlib import Path
from dataclasses import dataclass
import json
import re
import time

from contracts import canonical_digest, check_path, contract_uuid, dns_hostname, read_json, require


def cloudflare_ranges(directory: Path) -> tuple[date, list[str]]:
    """Read and validate the bundled Cloudflare CIDRs; never fetch during provisioning."""
    value = read_json(directory / "cloudflare-ranges.json")
    retrieved = date.fromisoformat(value["retrieved_at"])
    ranges = []
    for version in (4, 6):
        entries = value[f"ipv{version}"]
        require(isinstance(entries, list) and bool(entries), "invalid_edge_ranges", "Cloudflare ranges must include IPv4 and IPv6.")
        for raw in entries:
            network = ipaddress.ip_network(raw, strict=True)
            require(network.version == version and network.prefixlen > 0 and str(network) == raw,
                    "invalid_edge_ranges", "Cloudflare range has an invalid address family or CIDR.")
            ranges.append(raw)
    require(len(set(ranges)) == len(ranges), "invalid_edge_ranges", "Duplicate Cloudflare range.")
    return retrieved, ranges


def edge_findings(profile: dict, directory: Path) -> list[dict]:
    """Describe mandatory real-IP rendering and the age of its trust data."""
    retrieved, ranges = cloudflare_ranges(directory)
    checksum = hashlib.sha256("\n".join(ranges).encode()).hexdigest()
    findings = [{"kind": "change", "code": "nginx_edge_configuration", "detail":
                 f"Cloudflare real IP; origin lockdown {profile.get('edge_origin_lockdown', False)}; ranges sha256 {checksum}"}]
    if (date.today() - retrieved).days > 90:
        findings.append({"kind": "warning", "code": "cloudflare_ranges_stale", "detail":
                         "Cloudflare IP ranges are older than 90 days; review the published lists and update the versioned file."})
    if (date.today() - retrieved).days > 180:
        findings.append({"kind": "stop", "code": "cloudflare_ranges_expired", "detail":
                         "Review and update the bundled Cloudflare ranges before creating or changing a site. Existing traffic and renewal remain available."})
    return findings


# nginx's default variables_hash_bucket_size (64) fits names up to 46 bytes on
# 64-bit hosts; the shared http-level limit belongs to the host administrator.
NGINX_VARIABLE_MAX = 46


def nginx_variable(entry: dict, suffix: str) -> str:
    """Name a per-site nginx variable after the instance's f_<uuid> allocation."""
    variable = entry["allocations"]["database"] + "_" + suffix
    require(len(variable) <= NGINX_VARIABLE_MAX, "nginx_invalid", "The site variable exceeds nginx's default hash bucket.")
    return variable


def edge_template(entry: dict, directory: Path) -> dict[str, str]:
    """Render per-site real IP and optional origin lockdown from versioned ranges."""
    profile = entry["profile"]
    result = {"@EDGE_GEO@": "", "@EDGE_SERVER@": ""}
    _, ranges = cloudflare_ranges(directory)
    result["@EDGE_SERVER@"] = "\n".join("    set_real_ip_from " + cidr + ";" for cidr in ranges)
    result["@EDGE_SERVER@"] += "\n    real_ip_header CF-Connecting-IP;"
    result["@EDGE_SERVER@"] += "\n    real_ip_recursive off;"
    if profile.get("edge_origin_lockdown", False):
        variable = nginx_variable(entry, "edge_peer")
        result["@EDGE_GEO@"] = ("geo $realip_remote_addr $" + variable + " {\n    default 0;\n"
                                  + "\n".join("    " + cidr + " 1;" for cidr in [*ranges, "127.0.0.1/32", "::1/128"])
                                  + "\n}")
        result["@EDGE_SERVER@"] += "\n    if ($" + variable + " = 0) { return 444; }"
    return result


TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"
RUNTIME = "/run/foundry/edge"
STREAM_INCLUDE = "/etc/nginx/modules-enabled/90-foundry-stream.conf"


@dataclass(frozen=True)
class EdgeObservation:
    instance_id: str
    boot_id: str
    roles: tuple[str, ...]
    closed: bool
    sha256: str
    workers: tuple[tuple[int, int], ...]


def _runtime(host, instance_id):
    contract_uuid(instance_id)
    path = host.check_control_directory(host.paths.p(RUNTIME + "/" + instance_id))
    path.mkdir(parents=True, mode=0o755, exist_ok=True)
    return path


def _worker_identities(host):
    from listeners import process_members
    identities = []
    for pid in sorted(process_members(host, "nginx.service")):
        path = host.paths.p(f"/proc/{pid}/stat")
        try:
            if b"nginx: worker process" not in host.paths.p(f"/proc/{pid}/cmdline").read_bytes():
                continue
            # comm may contain spaces/parentheses; fields after its last ')' start at state.
            fields = path.read_text().rsplit(")", 1)[1].split()
            identities.append((pid, int(fields[19])))
        except FileNotFoundError:
            continue
    require(identities, "nginx_observation", "Nginx process identities are unavailable.")
    return tuple(identities)


def upstream_references(host):
    """Reserve literal upstreams in *all* sites, including foreign HTTP/stream sites.

    Dynamic/DNS upstreams cannot establish address isolation on this backend. They
    refuse feature admission rather than assuming only Foundry routes matter.
    """
    from listeners import SocketIdentity
    text = host.run(["nginx", "-T"], timeout=10).stdout
    require(len(text) <= 4 * 1024 * 1024, "nginx_observation", "Nginx configuration exceeds its bound.")
    references = set()
    in_upstream, depth = False, 0
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].strip()
        if re.match(r"upstream\s+\S+\s*\{", line):
            in_upstream = True
            depth = 0
        matches = re.findall(r"(?:proxy_pass|grpc_pass|fastcgi_pass|uwsgi_pass|scgi_pass)\s+([^;\s]+)", line)
        if in_upstream:
            matches += re.findall(r"\bserver\s+([^;\s]+)", line)
        for target in matches:
            if target.startswith("unix:"):
                continue
            target = re.sub(r"^[a-z]+://", "", target).split("/", 1)[0]
            address, separator, port = target.rpartition(":")
            # Named upstream groups are covered by their server declarations.
            if not separator and re.search(r"\bupstream\s+" + re.escape(target) + r"\s*\{", text):
                continue
            require(separator and port.isdigit() and "$" not in target,
                    "nginx_reference_unverified", "Dynamic or nonliteral nginx upstreams prevent backend isolation.")
            address = address.strip("[]")
            if address == "localhost":
                addresses = ("127.0.0.1", "::1")
            else:
                try:
                    addresses = (str(ipaddress.ip_address(address)),)
                except ValueError:
                    require(False, "nginx_reference_unverified", "DNS upstreams cannot prove private-address isolation.")
            references.update(SocketIdentity("tcp", ipaddress.ip_address(a).version, a, int(port)) for a in addresses)
        if in_upstream:
            depth += line.count("{") - line.count("}")
            if depth == 0:
                in_upstream = False
    return tuple(sorted(references, key=lambda s: (s.family, s.address, s.port)))


def retain_global_references(host):
    """Host-wide quarantine, including unaffected/foreign upstreams, until reboot.

    The first adoption requires old workers to drain naturally after a validated
    reload. Never kill foreign sessions to establish that baseline. A busy host
    may refuse until they drain. Subsequent changes capture before route writes.
    """
    from dataclasses import asdict
    from firewall import boot_id
    from listeners import SocketIdentity
    path = check_path(host.paths.p("/var/lib/foundry/network/nginx-references.json"), "file")
    boot = boot_id(host)
    references = upstream_references(host)
    value = read_json(path) if path.exists() else None
    if value is None or value["boot_id"] != boot:
        previous, _ = _reload(host)
        deadline = time.monotonic() + 10
        while set(previous) & set(_worker_identities(host)) and time.monotonic() < deadline:
            time.sleep(0.05)
        require(not set(previous) & set(_worker_identities(host)), "nginx_baseline_unverified",
                "Pre-adoption nginx sessions have not drained; fresh backend allocation remains blocked.")
        previous_references = ()
    else:
        previous_references = tuple(SocketIdentity(**v) for v in value["sockets"])
    combined = tuple(sorted(set((*previous_references, *references)), key=lambda s: (s.family, s.address, s.port)))
    require(len(combined) <= 65536, "nginx_reference_capacity", "Global upstream reservation capacity exhausted.")
    host.owned_write(path, json.dumps({"boot_id": boot, "sockets": [asdict(s) for s in combined]}, sort_keys=True) + "\n", mode=0o600)
    return combined


def _reload(host):
    previous = _worker_identities(host)
    host.run(["nginx", "-t", "-q"], timeout=10)
    host.run(["systemctl", "reload", "nginx"], timeout=10)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        current = _worker_identities(host)
        if set(current) - set(previous):
            return previous, current
        time.sleep(0.05)
    require(False, "nginx_reload_unverified", "No new nginx worker generation was observed; keep admission pending.")


def _top_level_include(text, name):
    """Only accept a literal include at top level; never rewrite foreign nginx text."""
    tokens = re.findall(r'"[^"\n]*"|\'[^\'\n]*\'|[^\s{};#]+|[{};]|#[^\n]*', text)
    depth = 0
    for index, token in enumerate(tokens):
        if token == "{":
            depth += 1
        elif token == "}":
            depth -= 1
        elif depth == 0 and tokens[index:index + 3] == ["include", name, ";"]:
            return True
    return False


def install_stream_include(host, *, reviewed_nginx_sha256):
    """Use Ubuntu's existing top-level modules include, after the stream module.

    Caller plans libnginx-mod-stream through the package owner. Other layouts
    require an operator-reviewed include; no foreign config is rewritten here.
    """
    main = check_path(host.paths.p("/etc/nginx/nginx.conf"), "file").read_text()
    require(hashlib.sha256(main.encode()).hexdigest() == reviewed_nginx_sha256
            and _top_level_include(main, "/etc/nginx/modules-enabled/*.conf"),
            "nginx_stream_integration", "Review the existing top-level modules include before stream integration.")
    path = check_path(host.paths.p(STREAM_INCLUDE), "file")
    content = (TEMPLATES / "nginx-stream.conf").read_text()
    require(not path.exists() or path.read_text() == content,
            "nginx_stream_ownership", "The stream include has foreign ownership.")
    existed = path.exists()
    host.owned_write(path, content)
    try:
        host.run(["nginx", "-t", "-q"], timeout=10)
    except Exception:
        if not existed:
            path.unlink()
        raise


def gate_http(instance_id, rendered_http):
    """Gate a caller-rendered owned site, preserving HTTP/upload/proxy directives.

    Lane A supplies its existing renderer's output using the selected resolution's
    HTTP endpoint. Only feature instances call this function, including rollback
    to a legacy declaration after feature adoption. Legacy sites remain byte-identical.
    """
    contract_uuid(instance_id)
    variable = "fdy_" + instance_id.replace("-", "") + "_open"
    require("# Foundry runtime admission" not in rendered_http and "server {\n" in rendered_http,
            "nginx_http_projection", "Supply the complete ungated owned HTTP projection.")
    prefix = (TEMPLATES / "nginx-http-admission.conf").read_text()
    prefix = prefix.replace("@INSTANCE@", instance_id).replace("@VARIABLE@", variable)
    return prefix + rendered_http.replace("server {\n", "server {\n    if ($" + variable + " = 0) { return 503; }\n")


def stream_projection(profile, resolution, lineage):
    """One runtime file per role; no certificate material enters application custody."""
    from certificates import CONFIG_DIR
    dns_hostname(lineage)
    endpoints = {(e.role, e.listener): e for e in resolution.endpoints}
    result = {}
    for name, binding in profile.listeners:
        role, listener = name.split(".")
        require((role, listener) in endpoints, "nginx_backend_missing", "Resolve every public listener's private backend first.")
        endpoint = endpoints[role, listener]
        backend = ("[" + endpoint.address + "]" if ":" in endpoint.address else endpoint.address) + ":" + str(endpoint.port)
        lines = []
        for address in binding.public_addresses:
            for port in binding.ports:
                bind = ("[" + address + "]" if ":" in address else address) + ":" + str(port)
                lines.append("    listen " + bind + " ssl" + (" ipv6only=on" if ":" in address else "") + ";")
        content = (TEMPLATES / "nginx-tcp-tls.conf").read_text()
        replacements = {"@INSTANCE@": profile.instance_id, "@LISTENER@": name, "@LISTEN@": "\n".join(lines),
                        "@LINEAGE@": lineage, "@BACKEND@": backend, "@CERTIFICATE_ROOT@": CONFIG_DIR,
                        "@SOURCES@": "\n".join("    allow " + cidr + ";" for cidr in binding.source_cidrs)}
        for token, value in replacements.items():
            content = content.replace(token, value)
        result["stream-" + name + ".conf"] = content
    return result


def _reference_path(host, instance_id):
    return check_path(host.paths.p("/var/lib/foundry/network/" + contract_uuid(instance_id) + "/references.json"), "file")


def retain_references(host, instance_id, resolution):
    """Journal before exposing nginx; interruption can only over-retain addresses.

    References deliberately survive reload and A-to-B-to-A. The admission owner
    includes these endpoints in its reservation inventory. Boot is the only
    automatic retirement boundary; no guess about a graceful worker exit is made.
    """
    from dataclasses import asdict
    from firewall import boot_id
    path = _reference_path(host, instance_id)
    boot = boot_id(host)
    value = read_json(path) if path.exists() else {"boot_id": boot, "endpoints": []}
    require(value["boot_id"] == boot, "nginx_boot_unprepared", "Prepare closed boot projections before retaining references.")
    endpoints = value["endpoints"]
    for endpoint in resolution.endpoints:
        document = asdict(endpoint)
        if document not in endpoints:
            endpoints.append(document)
    require(len(endpoints) <= 65536, "nginx_reference_capacity", "Retained reference capacity exhausted; reconcile before opening.")
    host.owned_write(path, json.dumps({"boot_id": boot, "endpoints": endpoints}, sort_keys=True) + "\n", mode=0o600)


def retained_references(host, instance_id):
    from contracts import AllocatedEndpoint
    from firewall import boot_id
    path = _reference_path(host, instance_id)
    require(path.exists(), "nginx_references_missing", "The complete nginx reference inventory is required.")
    value = read_json(path)
    require(value["boot_id"] == boot_id(host), "nginx_boot_unprepared", "Reference inventory belongs to another boot.")
    return tuple(AllocatedEndpoint.parse(e) for e in value["endpoints"])


def prepare_boot(host, instance_id):
    """Called by lane A's boot coordinator before nginx and before any start permit.

    Missing admission stays closed. This function never reads an archive to open a
    route and never touches legacy sites. Same-boot retries retain old references.
    """
    from firewall import boot_id
    directory = _runtime(host, instance_id)
    for path in directory.glob("*.conf"):
        check_path(path, "file").unlink()
    path = _reference_path(host, instance_id)
    boot = boot_id(host)
    if not path.exists() or read_json(path)["boot_id"] != boot:
        host.owned_write(path, json.dumps({"boot_id": boot, "endpoints": []}) + "\n", mode=0o600)


def observe_projection(host, instance_id, roles, *, expected=None):
    """No worker-exit assertion: the returned identities explicitly include old workers."""
    from firewall import boot_id
    directory = host.paths.p(RUNTIME + "/" + contract_uuid(instance_id))
    roles = tuple(sorted(set(roles)))
    contents = {}
    if directory.exists():
        for path in directory.glob("*.conf"):
            if path.name.startswith("http-open") and "http" in roles or any(path.name.startswith("stream-" + role + ".") for role in roles):
                contents[path.name] = check_path(path, "file").read_text()
    require(contents == (expected or {}), "nginx_projection_drift", "Selected runtime routes differ from their expected projection.")
    effective = host.run(["nginx", "-T"], timeout=10).stdout
    require(len(effective) <= 4 * 1024 * 1024 and (TEMPLATES / "nginx-stream.conf").read_text().strip() in effective,
            "nginx_stream_integration", "Effective nginx configuration lacks the owned top-level stream include.")
    if "http" in roles:
        require("# Foundry runtime admission " + instance_id in effective,
                "nginx_http_projection", "The feature HTTP site must have its persistent closed gate.")
    for content in contents.values():
        require(content.strip() in effective, "nginx_projection_drift", "Nginx did not read the selected runtime projection.")
    workers = _worker_identities(host)
    return EdgeObservation(instance_id, boot_id(host), roles, not contents,
                           canonical_digest({"instance_id": instance_id, "roles": roles,
                                             "configuration": effective, "workers": workers, "selected": contents}), workers)


def withdraw(host, instance_id, roles):
    retain_global_references(host)
    directory = _runtime(host, instance_id)
    for path in directory.glob("*.conf"):
        if path.name.startswith("http-open") and "http" in roles or any(path.name.startswith("stream-" + role + ".") for role in roles):
            check_path(path, "file").unlink()
    _reload(host)
    return observe_projection(host, instance_id, roles)


def open_runtime(host, profile, resolution, lineage, roles, *, authorize):
    """Lane A journals intent and revalidates health/authority through authorize.

    Firewall remains closed during this operation. Lane A opens it last and keeps
    the operation pending on any unknown result. Per-role operations retain other
    routes and old workers; closure proof covers only the supplied scope.
    """
    require(authorize() is True, "admission_required", "Fresh public-opening authority is required.")
    retain_global_references(host)
    roles = tuple(roles)
    require(set(roles) <= set(resolution.roles), "nginx_projection_scope", "Opening roles must belong to the resolution.")
    projection = {name: value for name, value in stream_projection(profile, resolution, lineage).items()
                  if any(name.startswith("stream-" + role + ".") for role in roles)}
    if "http" in roles:
        projection["http-open.conf"] = '"" 1;\n'
    retain_references(host, profile.instance_id, resolution)
    directory = _runtime(host, profile.instance_id)
    try:
        for name, content in projection.items():
            host.owned_write(directory / name, content)
        _reload(host)
        require(authorize() is True, "admission_required", "Public-opening authority changed.")
        return observe_projection(host, profile.instance_id, roles, expected=projection)
    except Exception:
        withdraw(host, profile.instance_id, roles)
        raise
