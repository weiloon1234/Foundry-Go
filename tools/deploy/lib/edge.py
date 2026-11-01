"""Versioned Cloudflare trust data and nginx rendering; no server-side fetching."""
from datetime import date
import hashlib
import ipaddress
from pathlib import Path

from contracts import PREFIX, read_json, require


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


def edge_template(entry: dict, directory: Path) -> dict[str, str]:
    """Render per-site real IP and optional origin lockdown from versioned ranges."""
    profile = entry["profile"]
    result = {"@EDGE_GEO@": "", "@EDGE_SERVER@": ""}
    _, ranges = cloudflare_ranges(directory)
    result["@EDGE_SERVER@"] = "\n".join("    set_real_ip_from " + cidr + ";" for cidr in ranges)
    result["@EDGE_SERVER@"] += "\n    real_ip_header CF-Connecting-IP;"
    result["@EDGE_SERVER@"] += "\n    real_ip_recursive off;"
    if profile.get("edge_origin_lockdown", False):
        variable = PREFIX.replace("-", "_") + "_" + entry["allocations"]["database"] + "_edge_peer"
        result["@EDGE_GEO@"] = ("geo $realip_remote_addr $" + variable + " {\n    default 0;\n"
                                  + "\n".join("    " + cidr + " 1;" for cidr in [*ranges, "127.0.0.1/32", "::1/128"])
                                  + "\n}")
        result["@EDGE_SERVER@"] += "\n    if ($" + variable + " = 0) { return 444; }"
    return result
