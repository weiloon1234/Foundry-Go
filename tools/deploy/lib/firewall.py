"""Narrow UFW integration for iptables legacy/nft; permits are never persisted.

Only an already active dual-family default-deny host is supported. Unknown packet
paths, other netfilter owners and foreign allows refuse without modifying them.
Call under the admission owner's host/instance locks and journal intent first.
"""
from dataclasses import asdict, dataclass
import ipaddress
import hashlib
import json
from pathlib import Path
import re
import shlex
from collections import Counter

from contracts import POSTGRES_PORT, REDIS_PORT, canonical_digest, check_path, contract_uuid, require

TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"
FILES = ("/etc/default/ufw", "/etc/ufw/ufw.conf", "/etc/ufw/before.rules", "/etc/ufw/user.rules",
         "/etc/ufw/after.rules", "/etc/ufw/before6.rules", "/etc/ufw/user6.rules", "/etc/ufw/after6.rules")
# Ubuntu ufw 0.36.2-{6,9build1,10}; see tests/network_fixtures/ufw/README.md.
# Unknown reload code cannot substantiate our reconstruction of its effects.
RELOAD_FILES = {
    "/usr/lib/ufw/ufw-init": "c98f19dd0a68e94f729acdcba34fe48244dbfacdfa8943f257215d2ae351f80a",
    "/usr/lib/ufw/ufw-init-functions": "5865fbffc0c644d97e5fc6906c648ce6e4f23710ea0ff57c573ed680999d2cc4",
}


def _namespace(family):
    require(family in (4, 6), "firewall_unsupported", "Unknown firewall address family.")
    return "ufw6" if family == 6 else "ufw"


def _settings(text):
    """UFW sources these files as shell; only literal assignments are supported."""
    result = {}
    allowed = {"IPV6", "DEFAULT_INPUT_POLICY", "DEFAULT_OUTPUT_POLICY", "DEFAULT_FORWARD_POLICY",
               "DEFAULT_APPLICATION_POLICY", "MANAGE_BUILTINS", "IPT_SYSCTL", "IPT_MODULES", "ENABLED", "LOGLEVEL"}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        fields = shlex.split(line, comments=True)
        require(len(fields) == 1 and "=" in fields[0], "firewall_unsupported", "UFW settings must be literal assignments.")
        name, value = fields[0].split("=", 1)
        require(name in allowed and name not in result and not any(c in value for c in "$`\\;\n"),
                "firewall_unsupported", "Unsupported or duplicate UFW setting.")
        result[name] = value
    return result


def reload_graph(files, family, settings):
    """Replay the validated ufw-init-functions graph without executing shell code.

    Restore declarations reset chains; user.rules may redefine logging helpers.
    UFW appends the user hooks after before/after/user restore, in that order.
    """
    prefix, suffix = _namespace(family), "6" if family == 6 else ""
    chains = {prefix + "-logging-" + kind: [] for kind in ("deny", "allow")}
    stages = ("before-logging", "before", "after", "after-logging", "reject", "track")
    for direction in ("input", "output", "forward"):
        for stage in (*stages, "user", "user-logging"):
            chains[f"{prefix}-{stage}-{direction}"] = []
        policy = settings["DEFAULT_" + direction.upper() + "_POLICY"]
        chains[f"{prefix}-skip-to-policy-{direction}"] = [("-j", policy)]
        if policy == "ACCEPT":
            chains[f"{prefix}-track-{direction}"] = [("-p", p, "-m", "conntrack", "--ctstate", "NEW", "-j", "ACCEPT") for p in ("tcp", "udp")]
        chains[direction.upper()] = [("-j", f"{prefix}-{stage}-{direction}") for stage in stages]
    for name in ("user-limit", "user-limit-accept"):
        chains[prefix + "-" + name] = []
    for stem in ("before", "after", "user"):
        part, policies = parse_rules(files[f"/etc/ufw/{stem}{suffix}.rules"])
        require(all(p == "-" for p in policies.values()) and not {"INPUT", "OUTPUT", "FORWARD"} & part.keys(),
                "firewall_unsupported", "Persistent UFW files cannot override built-in policies.")
        require(all(f"{prefix}-{stem}-{direction}" in part for direction in ("input", "output", "forward")),
                "firewall_unsupported", "UFW reload paths have the wrong family or are incomplete.")
        chains.update(part)
    for direction in ("input", "output", "forward"):
        chains[prefix + "-before-" + direction].append(("-j", prefix + "-user-" + direction))
    return chains


def require_nft_layout(document, live, backends):
    """Account for every nft object against the complete iptables-save views.

    iptables-nft's compatibility decoder is the rule semantics authority. Reject
    diagnostics, missing rules, alternate hooks and native objects it cannot expose.
    Counts are per chain, not a global count that could conceal an omitted rule.
    """
    require(isinstance(document, dict) and isinstance(document.get("nftables"), list),
            "firewall_unsupported", "Nftables inventory is incomplete.")
    tables, chains, counts = set(), {}, Counter()
    for item in document["nftables"]:
        require(isinstance(item, dict) and len(item) == 1, "firewall_unsupported", "Unknown nftables object.")
        kind, obj = next(iter(item.items()))
        if kind == "metainfo":
            continue
        require(kind in ("table", "chain", "rule") and isinstance(obj, dict),
                "firewall_unsupported", "Native nftables objects are outside the supported UFW layout.")
        family = {"ip": 4, "ip6": 6}.get(obj.get("family"))
        require(family in live and backends[family] == "nf_tables" and obj.get("table", obj.get("name")) == "filter",
                "firewall_unsupported", "Unrelated native nftables tables or conflicting backends are present.")
        if kind == "table":
            require(family not in tables and not obj.get("flags"), "firewall_unsupported", "Unexpected nftables table flags.")
            tables.add(family)
        elif kind == "chain":
            name = obj.get("name")
            require(name in live[family]["chains"] and (family, name) not in chains,
                    "firewall_unsupported", "Nftables chain is absent from the iptables view.")
            if name in ("INPUT", "OUTPUT", "FORWARD"):
                require(obj.get("type") == "filter" and obj.get("hook") == name.lower()
                        and obj.get("prio") == 0 and obj.get("policy", "").upper() == live[family]["policies"][name],
                        "firewall_unsupported", "Nftables base-chain policy or ordering differs from iptables.")
            else:
                require(name.startswith((_namespace(family) + "-", "fdy-"))
                        and not {"type", "hook", "prio", "policy", "dev", "flags"} & obj.keys(),
                        "firewall_unsupported", "A native or separately hooked chain conflicts with UFW.")
            chains[family, name] = obj
        else:
            name, expressions = obj.get("chain"), obj.get("expr")
            require(name in live[family]["chains"] and isinstance(expressions, list) and expressions,
                    "firewall_unsupported", "Nftables rule is absent from the iptables view.")
            require(all(isinstance(e, dict) and len(e) == 1 and next(iter(e)) in
                        {"match", "counter", "jump", "goto", "accept", "drop", "reject", "return", "log", "limit", "xt"}
                        for e in expressions), "firewall_unsupported", "Unsupported native nftables rule expression.")
            counts[family, name] += 1
    expected_families = {f for f, backend in backends.items() if backend == "nf_tables"}
    require(tables == expected_families and set(chains) == {(f, n) for f in expected_families for n in live[f]["chains"]}
            and all(counts[f, n] == len(rules) for f in expected_families for n, rules in live[f]["chains"].items()),
            "firewall_unsupported", "Nftables and iptables do not expose the same complete rule inventory.")


@dataclass(frozen=True)
class FirewallObservation:
    instance_id: str
    boot_id: str
    closed: bool
    sha256: str
    ports: tuple[int, ...]
    sockets: tuple


def chain_name(instance_id):
    contract_uuid(instance_id)
    # Full UUID identity is retained in the owned block; kernel chain names are bounded.
    return "fdy-" + hashlib.sha256(instance_id.encode()).hexdigest()[:24]


def _read(host, path):
    path = check_path(host.paths.p(path), "file")
    require(path.is_file() and not path.stat().st_mode & 0o022
            and (host.paths.root != Path("/") or path.stat().st_uid == 0),
            "firewall_unsupported", "Firewall configuration must be owned and inspectable.")
    value = path.read_text()
    require(len(value) <= 1024 * 1024, "firewall_unsupported", "Firewall configuration exceeds its bound.")
    return value


def boot_id(host):
    return contract_uuid(host.paths.p("/proc/sys/kernel/random/boot_id").read_text().strip())


def parse_rules(text):
    """Accept iptables-save/restore syntax, retaining chain order and exact rules."""
    require(len(text) <= 4 * 1024 * 1024, "firewall_unsupported", "Kernel rules exceed their bound.")
    chains, policies, table = {}, {}, None
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("*"):
            require(table is None, "firewall_unsupported", "Nested firewall tables.")
            table = line[1:]
        elif line == "COMMIT":
            table = None
        elif line.startswith(":"):
            name, policy, counter = line[1:].split()
            require(table is not None, "firewall_unsupported", "Unscoped firewall chain.")
            if table != "filter":
                require(policy in ("ACCEPT", "-"), "firewall_unsupported", "Unsupported packet path.")
                continue
            require(name not in chains, "firewall_unsupported", "Duplicate firewall chain.")
            chains[name], policies[name] = [], policy
        else:
            fields = tuple(shlex.split(line))
            require(table == "filter" and len(fields) >= 4 and fields[0] in ("-A", "-I") and fields[1] in chains,
                    "firewall_unsupported", "Only inspectable filter rules are supported; NAT/raw/mangle rules refuse.")
            rules, body = chains[fields[1]], fields[2:]
            if fields[0] == "-A":
                rules.append(body)
            else:
                position = 1
                if body[0].lstrip("+-").isdigit():
                    require(re.fullmatch(r"[1-9][0-9]*", body[0]) is not None,
                            "firewall_unsupported", "Invalid firewall insertion position.")
                    position, body = int(body[0]), body[1:]
                require(1 <= position <= len(rules) + 1 and len(body) >= 2 and body[0].startswith("-"),
                        "firewall_unsupported", "Unsupported firewall insertion.")
                rules.insert(position - 1, body)
    require(table is None and chains, "firewall_unsupported", "Incomplete firewall rules.")
    return chains, policies


def _argument(rule, *names):
    values = [rule[i + 1] for i, field in enumerate(rule[:-1]) if field in names]
    require(len(values) <= 1, "firewall_unsupported", "Ambiguous firewall match.")
    return values[0] if values else None


def _port_matches(spec, ports):
    try:
        for item in spec.split(","):
            lower, _, upper = item.partition(":")
            if any(int(lower) <= port <= int(upper or lower) for port in ports):
                return True
        return False
    except ValueError:
        require(False, "firewall_unsupported", "Firewall ports must be numeric.")


def require_denied(chains, roots, ports, *, owned=None, allowed=()):
    """Conservative reachability for new external TCP, including interface paths.

    Rules that cannot match TCP/new/the exclusive ports are irrelevant. All other
    foreign ACCEPTs refuse, including broader sources and interface-specific rules.
    Established sessions are handled by backend exit, never claimed closed here.
    """
    visited = set()
    def walk(name, ancestors=()):
        require(name in chains and name not in ancestors, "firewall_unsupported", "Unknown or recursive firewall jump.")
        if name in visited:
            return
        visited.add(name)
        for rule in chains[name]:
            require("!" not in rule, "firewall_unsupported", "Negated firewall matches are unsupported.")
            if name == owned:
                require(rule in allowed, "firewall_drift", "Owned runtime exposure differs from the selected projection.")
                continue
            protocol = _argument(rule, "-p", "--protocol")
            if protocol in ("udp", "icmp", "ipv6-icmp", "icmpv6", "esp", "ah", "igmp", "41", "2"):
                continue
            require(protocol in (None, "tcp", "all", "6"), "firewall_unsupported", "Unknown firewall protocol.")
            if _argument(rule, "-i", "--in-interface") == "lo":
                continue
            states = _argument(rule, "--ctstate", "--state")
            if states and set(states.split(",")) <= {"ESTABLISHED", "RELATED", "INVALID"}:
                continue
            dport = _argument(rule, "--dport", "--destination-port", "--dports")
            if dport and not _port_matches(dport, ports):
                continue
            target = _argument(rule, "-j", "--jump", "-g", "--goto")
            require(target is not None, "firewall_unsupported", "Uninspectable firewall target.")
            if target in ("DROP", "REJECT", "RETURN", "LOG", "NFLOG"):
                continue
            require(target != "ACCEPT", "firewall_foreign_allow", "A foreign rule can admit an exclusive TCP port.")
            walk(target, (*ancestors, name))
    for root in roots:
        walk(root)
    if owned:
        require(owned in visited and tuple(chains[owned]) == tuple(allowed),
                "firewall_drift", "The exact owned runtime chain must be reachable.")


def exposure_rules(instance_id, claims, bindings, family):
    chain = chain_name(instance_id)
    rules = []
    for claim in claims:
        require(claim.instance_id == instance_id and claim.owner == "nginx" and claim.socket.transport == "tcp",
                "firewall_scope", "Only this instance's exclusive TLS sockets may be exposed.")
        if claim.socket.family != family:
            continue
        binding = dict(bindings)[claim.role + "." + claim.listener]
        for source in binding.source_cidrs:
            if ipaddress.ip_network(source).version == family:
                destination = str(ipaddress.ip_network(claim.socket.address + ("/32" if family == 4 else "/128")))
                source_match = ("-s", source) if ipaddress.ip_network(source).prefixlen else ()
                rules.append((*source_match, "-d", destination, "-p", "tcp", "-m", "tcp", "--dport",
                              str(claim.socket.port), "-j", "ACCEPT"))
    return chain, tuple(dict.fromkeys(rules))


def _ports(claims):
    ports = tuple(sorted({claim.socket.port for claim in claims}))
    require(ports and not set(ports) & {22, 80, 443, POSTGRES_PORT, REDIS_PORT},
            "firewall_scope", "Firewall ownership requires exclusive nonshared TCP ports.")
    return ports


def _network_policy(routes, addresses):
    """Bind network configuration, not iproute2's traffic/age/link observations.

    Route destinations, gateways, devices, tables, metrics and address assignments
    remain covered. Countdown values and counters describe an observation instant,
    not a different policy. Expiry still changes the digest when an entry vanishes.
    """
    def ordered(values):
        return sorted(values, key=lambda value: json.dumps(value, sort_keys=True))
    route_observations = {"expires", "cache", "used", "lastuse", "users", "refcnt", "age"}
    interface_observations = {"stats", "stats64", "operstate", "carrier", "carrier_changes",
                              "carrier_up_count", "carrier_down_count"}
    address_observations = {"valid_life_time", "preferred_life_time", "tentative", "dadfailed", "deprecated", "optimistic"}
    interfaces = []
    for interface in addresses:
        value = {k: v for k, v in interface.items() if k not in interface_observations}
        if "flags" in value:
            value["flags"] = sorted(set(value["flags"]) - {"LOWER_UP", "RUNNING", "DORMANT"})
        value["addr_info"] = ordered({k: sorted(set(v) - address_observations) if k == "flags" else v for k, v in address.items()
                                      if k not in address_observations} for address in interface.get("addr_info", []))
        interfaces.append(value)
    return {"routes": {family: ordered({k: v for k, v in route.items() if k not in route_observations}
                                      for route in entries) for family, entries in routes.items()},
            "addresses": ordered(interfaces)}


def observe(host, instance_id, claims, *, bindings=(), opened=False, integrated=True, retained_claims=()):
    """Read live policy, reload policy, interfaces/routes and other firewall owners."""
    chain = chain_name(instance_id)
    if not claims:
        return FirewallObservation(instance_id, boot_id(host), True,
                                   canonical_digest({"instance": instance_id, "public_sockets": []}), (), ())
    ports = _ports(claims)
    require(not set(ports) & {c.socket.port for c in retained_claims},
            "firewall_scope", "Retained listeners must not overlap the affected socket union.")
    require(all(c.instance_id == instance_id and c.owner == "nginx" and c.socket.transport == "tcp" for c in claims),
            "firewall_scope", "The firewall observation must name one instance's TCP edge sockets.")
    status = host.run(["ufw", "status"], timeout=5).stdout
    require(status.splitlines()[:1] == ["Status: active"], "firewall_inactive", "Configure and activate default-deny UFW explicitly.")
    files = {path: _read(host, path) for path in FILES}
    if integrated:
        for family, path in ((4, "/etc/ufw/before.rules"), (6, "/etc/ufw/before6.rules")):
            _require_owned_chain(files[path], instance_id, family=family)
    settings = _settings(files[FILES[0]])
    control = _settings(files[FILES[1]])
    require(settings.get("IPV6", "").lower() == "yes"
            and settings.get("DEFAULT_INPUT_POLICY") == settings.get("DEFAULT_FORWARD_POLICY") == "DROP"
            and settings.get("DEFAULT_OUTPUT_POLICY") in ("ACCEPT", "DROP")
            and settings.get("MANAGE_BUILTINS", "no") == "no"
            and settings.get("IPT_MODULES", "") == ""
            and settings.get("IPT_SYSCTL", "/etc/ufw/sysctl.conf") == "/etc/ufw/sysctl.conf",
            "firewall_default_deny", "UFW must persist dual-family default-deny without changing foreign built-ins.")
    require(control.get("ENABLED", "").lower() == "yes", "firewall_inactive", "UFW must restore its closed policy at boot.")
    for path, checksum in RELOAD_FILES.items():
        content = _read(host, path)
        require(hashlib.sha256(content.encode()).hexdigest() == checksum,
                "firewall_unsupported", "UFW reload machinery differs from the reviewed Ubuntu layout.")
        files[path] = content
    for path in ("/etc/ufw/before.init", "/etc/ufw/after.init"):
        hook = check_path(host.paths.p(path), "file")
        if hook.exists():
            # The reviewed ufw-init-functions invokes hooks only with shell -x.
            files[path] = _read(host, path)
            require(not hook.stat().st_mode & 0o111, "firewall_unsupported",
                    "Executable UFW reload hooks require a separately supported layout.")
    live, backends = {}, {}
    for family, binary in ((4, "iptables"), (6, "ip6tables")):
        version = host.run([binary, "--version"], timeout=5).stdout
        backend = re.search(r"\((legacy|nf_tables)\)", version)
        require(backend is not None, "firewall_unsupported", "Unknown iptables backend.")
        backends[family] = backend.group(1)
        response = host.run([binary + "-save"], timeout=5)
        require(not response.stderr.strip() and not re.search(r"warning|incompatible|unsupported", response.stdout, re.I),
                "firewall_unsupported", "The iptables compatibility decoder could not expose a complete ruleset.")
        chains, policies = parse_rules(response.stdout)
        require(policies.get("INPUT") == policies.get("FORWARD") == "DROP",
                "firewall_default_deny", "Live input and routed policies must both default to DROP.")
        _, expected = exposure_rules(instance_id, (*retained_claims, *(claims if opened else ())), bindings, family)
        require_denied(chains, ("INPUT", "FORWARD"), ports, owned=chain if integrated else None, allowed=expected)
        persisted = reload_graph(files, family, settings)
        require_denied(persisted, ("INPUT", "FORWARD"), ports, owned=chain if integrated else None)
        live[family] = {"chains": chains, "policies": policies}
    nft = json.loads(host.run(["nft", "-j", "list", "ruleset"], timeout=5).stdout)
    require_nft_layout(nft, live, backends)
    for family, binary in ((4, "iptables"), (6, "ip6tables")):
        again = host.run([binary + "-save"], timeout=5)
        require(not again.stderr.strip() and not re.search(r"warning|incompatible|unsupported", again.stdout, re.I)
                and parse_rules(again.stdout) == (live[family]["chains"], live[family]["policies"]),
                "firewall_drift", "Kernel rules changed during the cross-backend observation.")
    routes = {}
    for family in (4, 6):
        routes[str(family)] = json.loads(host.run(["ip", "-j", "-" + str(family), "route", "show", "table", "all"], timeout=5).stdout)
    addresses = json.loads(host.run(["ip", "-j", "address", "show"], timeout=5).stdout)
    local = {a["local"] for interface in addresses for a in interface.get("addr_info", [])}
    require(all(c.socket.address in local for c in claims), "firewall_address_missing", "Bind only explicit addresses present on this host.")
    return FirewallObservation(instance_id, boot_id(host), not opened,
                               canonical_digest({"instance_id": instance_id,
                                   "scope": sorted((asdict(c.socket) for c in claims), key=lambda v: (v["family"], v["address"], v["port"])),
                                   "live": live, "backends": backends, "persistent": files,
                                   **_network_policy(routes, addresses)}),
                               ports, tuple(c.socket for c in claims))


def persistent_projection(text, instance_id, *, family=4):
    """Add only owned chain/jump lines; preserve every existing byte and order."""
    chain = chain_name(instance_id)
    marker = "# Foundry network " + instance_id
    before_input = _namespace(family) + "-before-input"
    if marker in text:
        require(text.count(marker + " chain") == text.count(marker + " jump") == 1
                and f":{chain} - [0:0]\n" in text and f"-A {before_input} -j {chain}\n" in text,
                "firewall_ownership", "Owned UFW integration has drifted.")
        return text
    require(chain not in text and text.count("*filter\n") == 1 and "-A " + before_input + " " in text,
            "firewall_unsupported", "UFW has no suitable explicit before-input integration.")
    text = text.replace("*filter\n", "*filter\n" + marker + " chain\n:" + chain + " - [0:0]\n", 1)
    position = text.index("-A " + before_input + " ")
    return text[:position] + marker + " jump\n-A " + before_input + " -j " + chain + "\n" + text[position:]


def _require_owned_chain(text, instance_id, *, family=4):
    marker, chain = "# Foundry network " + instance_id, chain_name(instance_id)
    before_input = _namespace(family) + "-before-input"
    require(text.count(marker + " chain\n:" + chain + " - [0:0]\n") == 1
            and text.count(marker + " jump\n-A " + before_input + " -j " + chain + "\n") == 1,
            "firewall_ownership", "Only the exact instance-owned chain may be changed.")


def install_closed(host, instance_id, claims, *, reviewed_sha256):
    before = observe(host, instance_id, claims, integrated=False)
    require(before.sha256 == reviewed_sha256, "firewall_plan_stale", "Firewall state changed after review.")
    # Both projections are validated before the first write. A partial write contains no allows.
    updates = [(path, persistent_projection(_read(host, path), instance_id, family=family))
               for family, path in ((4, "/etc/ufw/before.rules"), (6, "/etc/ufw/before6.rules"))]
    for path, content in updates:
        host.owned_write(host.paths.p(path), content, mode=0o640)
    host.run(["ufw", "reload"], timeout=30)
    return observe(host, instance_id, claims)


def withdraw(host, instance_id, claims, *, retained_claims=(), bindings=()):
    """Attempt both families even if one fails; no partial-success evidence."""
    if not claims:
        return observe(host, instance_id, claims)
    for family, path in ((4, "/etc/ufw/before.rules"), (6, "/etc/ufw/before6.rules")):
        _require_owned_chain(_read(host, path), instance_id, family=family)
    failure = None
    for family, binary in ((4, "iptables"), (6, "ip6tables")):
        try:
            chain, rules = exposure_rules(instance_id, retained_claims, bindings, family)
            _replace_runtime(host, binary, chain, rules)
        except Exception as error:
            failure = error
    if failure:
        raise failure
    return observe(host, instance_id, claims, retained_claims=retained_claims, bindings=bindings)


def _replace_runtime(host, binary, chain, rules):
    content = (TEMPLATES / "firewall-runtime.rules").read_text().replace("@CHAIN@", chain)
    content = content.replace("@RULES@", "\n".join("-A " + chain + " " + " ".join(rule) for rule in rules))
    host.run([binary + "-restore", "--noflush", "--wait", "5"], input=content, timeout=10)


def open_runtime(host, instance_id, claims, bindings, *, closed_sha256, authorize, retained_claims=()):
    """authorize re-observes lane A's admission/permits, backend identities and health.

    A caller must journal opening intent first. Any unknown result remains pending;
    this function cannot commit admission. Reload/reboot destroys these allows.
    """
    require(observe(host, instance_id, claims, bindings=bindings, retained_claims=retained_claims).sha256 == closed_sha256,
            "firewall_plan_stale", "Closed firewall evidence changed before opening.")
    require(authorize() is True, "admission_required", "Fresh public-opening authority is required.")
    if not claims:
        return observe(host, instance_id, claims)
    try:
        for family, binary in ((4, "iptables"), (6, "ip6tables")):
            chain, rules = exposure_rules(instance_id, (*retained_claims, *claims), bindings, family)
            _replace_runtime(host, binary, chain, rules)
        require(authorize() is True, "admission_required", "Public-opening authority changed.")
        return observe(host, instance_id, claims, bindings=bindings, opened=True, retained_claims=retained_claims)
    except Exception:
        withdraw(host, instance_id, claims, retained_claims=retained_claims, bindings=bindings)
        raise
