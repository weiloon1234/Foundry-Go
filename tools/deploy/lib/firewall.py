"""Narrow UFW/iptables-legacy integration; runtime permits are never persisted.

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

from contracts import POSTGRES_PORT, REDIS_PORT, canonical_digest, check_path, contract_uuid, require

TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"
FILES = ("/etc/default/ufw", "/etc/ufw/ufw.conf", "/etc/ufw/before.rules", "/etc/ufw/user.rules",
         "/etc/ufw/after.rules", "/etc/ufw/before6.rules", "/etc/ufw/user6.rules", "/etc/ufw/after6.rules")


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
            require(table == "filter" and len(fields) >= 4 and fields[0] == "-A" and fields[1] in chains,
                    "firewall_unsupported", "Only inspectable filter rules are supported; NAT/raw/mangle rules refuse.")
            chains[fields[1]].append(fields[2:])
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
        for path in ("/etc/ufw/before.rules", "/etc/ufw/before6.rules"):
            _require_owned_chain(files[path], instance_id)
    for key, value in (("IPV6", "yes"), ("DEFAULT_INPUT_POLICY", "DROP"), ("DEFAULT_FORWARD_POLICY", "DROP")):
        require(re.findall(r"^" + key + r'=[\"\']?([^\"\'\n]+)[\"\']?$', files[FILES[0]], re.M) == [value],
                "firewall_default_deny", "UFW must persist default-deny IPv4 and IPv6 input and routing.")
    require(re.findall(r"^ENABLED=(\w+)$", files[FILES[1]], re.M) == ["yes"],
            "firewall_inactive", "UFW must restore its closed policy at boot.")
    nft = json.loads(host.run(["nft", "-j", "list", "ruleset"], timeout=5).stdout)
    require(isinstance(nft, dict) and isinstance(nft.get("nftables"), list)
            and not [v for v in nft["nftables"] if set(v) != {"metainfo"}],
            "firewall_unsupported", "This backend requires iptables-legacy with no concurrent nftables policy.")
    for path in ("/etc/ufw/before.init", "/etc/ufw/after.init"):
        require(not host.paths.p(path).exists(), "firewall_unsupported", "Custom UFW reload hooks require a separately supported layout.")
    live = {}
    for family, binary, suffix in ((4, "iptables", ""), (6, "ip6tables", "6")):
        require("(legacy)" in host.run([binary, "--version"], timeout=5).stdout,
                "firewall_unsupported", "The reviewed UFW backend is iptables-legacy for both families.")
        text = host.run([binary + "-save"], timeout=5).stdout
        chains, policies = parse_rules(text)
        require(policies.get("INPUT") == policies.get("FORWARD") == "DROP",
                "firewall_default_deny", "Live input and routed policies must both default to DROP.")
        _, expected = exposure_rules(instance_id, (*retained_claims, *(claims if opened else ())), bindings, family)
        require_denied(chains, ("INPUT", "FORWARD"), ports, owned=chain if integrated else None, allowed=expected)
        persisted = {}
        for stem in ("before", "user", "after"):
            part, _ = parse_rules(files[f"/etc/ufw/{stem}{suffix}.rules"])
            require(not persisted.keys() & part.keys(), "firewall_unsupported", "Ambiguous persistent chain ownership.")
            persisted.update(part)
        roots = tuple(n for n in persisted if n in ("ufw-before-input", "ufw-user-input", "ufw-after-input",
                                                    "ufw-before-forward", "ufw-user-forward", "ufw-after-forward"))
        require(len(roots) == 6, "firewall_unsupported", "UFW reload paths are incomplete.")
        require(not {"INPUT", "OUTPUT", "FORWARD"} & persisted.keys(),
                "firewall_unsupported", "Persistent UFW files cannot override built-in chain policy.")
        for name in ("ufw-skip-to-policy-input", "ufw-skip-to-policy-forward"):
            require(chains.get(name) == [("-j", "DROP")], "firewall_default_deny",
                    "UFW policy helper chains must implement the configured DROP policy.")
            if name not in persisted:
                persisted[name] = [("-j", "DROP")]
        require_denied(persisted, roots, ports, owned=chain if integrated else None)
        # Counters are mutable and never part of a permit's policy fingerprint.
        live[str(family)] = {"chains": chains, "policies": policies}
    routes = {}
    for family in (4, 6):
        routes[str(family)] = json.loads(host.run(["ip", "-j", "-" + str(family), "route", "show", "table", "all"], timeout=5).stdout)
    addresses = json.loads(host.run(["ip", "-j", "address", "show"], timeout=5).stdout)
    local = {a["local"] for interface in addresses for a in interface.get("addr_info", [])}
    require(all(c.socket.address in local for c in claims), "firewall_address_missing", "Bind only explicit addresses present on this host.")
    return FirewallObservation(instance_id, boot_id(host), not opened,
                               canonical_digest({"instance_id": instance_id,
                                   "scope": sorted((asdict(c.socket) for c in claims), key=lambda v: (v["family"], v["address"], v["port"])),
                                   "live": live, "persistent": files, "routes": routes, "addresses": addresses}),
                               ports, tuple(c.socket for c in claims))


def persistent_projection(text, instance_id):
    """Add only owned chain/jump lines; preserve every existing byte and order."""
    chain = chain_name(instance_id)
    marker = "# Foundry network " + instance_id
    if marker in text:
        require(text.count(marker + " chain") == text.count(marker + " jump") == 1
                and f":{chain} - [0:0]\n" in text and f"-A ufw-before-input -j {chain}\n" in text,
                "firewall_ownership", "Owned UFW integration has drifted.")
        return text
    require(chain not in text and text.count("*filter\n") == 1 and "-A ufw-before-input " in text,
            "firewall_unsupported", "UFW has no suitable explicit before-input integration.")
    text = text.replace("*filter\n", "*filter\n" + marker + " chain\n:" + chain + " - [0:0]\n", 1)
    position = text.index("-A ufw-before-input ")
    return text[:position] + marker + " jump\n-A ufw-before-input -j " + chain + "\n" + text[position:]


def _require_owned_chain(text, instance_id):
    marker, chain = "# Foundry network " + instance_id, chain_name(instance_id)
    require(text.count(marker + " chain\n:" + chain + " - [0:0]\n") == 1
            and text.count(marker + " jump\n-A ufw-before-input -j " + chain + "\n") == 1,
            "firewall_ownership", "Only the exact instance-owned chain may be changed.")


def install_closed(host, instance_id, claims, *, reviewed_sha256):
    before = observe(host, instance_id, claims, integrated=False)
    require(before.sha256 == reviewed_sha256, "firewall_plan_stale", "Firewall state changed after review.")
    # Both projections are validated before the first write. A partial write contains no allows.
    updates = [(path, persistent_projection(_read(host, path), instance_id))
               for path in ("/etc/ufw/before.rules", "/etc/ufw/before6.rules")]
    for path, content in updates:
        host.owned_write(host.paths.p(path), content, mode=0o640)
    host.run(["ufw", "reload"], timeout=30)
    return observe(host, instance_id, claims)


def withdraw(host, instance_id, claims, *, retained_claims=(), bindings=()):
    """Attempt both families even if one fails; no partial-success evidence."""
    if not claims:
        return observe(host, instance_id, claims)
    for path in ("/etc/ufw/before.rules", "/etc/ufw/before6.rules"):
        _require_owned_chain(_read(host, path), instance_id)
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
