"""Standard Ubuntu UFW inputs plus an in-memory kernel view; never runs a firewall."""
from pathlib import Path
import shlex

ROOT = Path(__file__).parent / "network_fixtures/ufw"


def rule_file(stem, instance_id=None, family=4):
    import firewall
    suffix = "6" if family == 6 else ""
    prefix = "ufw" + suffix
    text = (ROOT / (stem + suffix + ".rules")).read_text()
    if stem == "user":
        text = text.replace("### RULES ###", "### RULES ###\n-A " + prefix + "-user-input -p tcp --dport 22 -j ACCEPT")
    if instance_id and stem == "before":
        chain = firewall.chain_name(instance_id)
        marker = "# Foundry network " + instance_id
        text = text.replace("*filter\n", "*filter\n" + marker + " chain\n:" + chain + " - [0:0]\n")
        index = text.index("-A " + prefix + "-before-input")
        text = text[:index] + marker + " jump\n-A " + prefix + "-before-input -j " + chain + "\n" + text[index:]
    return text


def live_rules(instance_id, family=4, open_rules=()):
    import firewall
    prefix = "ufw6" if family == 6 else "ufw"
    chains = {}
    # The primary/secondary graph is created by ufw-init-functions, not before.rules.
    stages = ("before-logging", "before", "after", "after-logging", "reject", "track")
    for direction in ("input", "output", "forward"):
        for stage in stages:
            chains[f"{prefix}-{stage}-{direction}"] = []
        chains[direction.upper()] = [("-j", f"{prefix}-{stage}-{direction}") for stage in stages]
        chains[f"{prefix}-skip-to-policy-{direction}"] = [("-j", "ACCEPT" if direction == "output" else "DROP")]
        chains[f"{prefix}-user-logging-{direction}"] = []
    chains[prefix + "-logging-deny"] = []
    chains[prefix + "-logging-allow"] = []
    chains[prefix + "-track-output"] = [("-p", p, "-m", "conntrack", "--ctstate", "NEW", "-j", "ACCEPT") for p in ("tcp", "udp")]
    for stem in ("before", "after", "user"):
        chains.update(firewall.parse_rules(rule_file(stem, instance_id, family))[0])
    for direction in ("input", "output", "forward"):
        chains[prefix + "-before-" + direction].append(("-j", prefix + "-user-" + direction))
    chains[firewall.chain_name(instance_id)] = list(open_rules)
    text = "*filter\n"
    for name in chains:
        policy = "DROP" if name in ("INPUT", "FORWARD") else "ACCEPT" if name == "OUTPUT" else "-"
        text += f":{name} {policy} [0:0]\n"
    for name, rules in chains.items():
        for rule in rules:
            text += "-A " + name + " " + shlex.join(rule) + "\n"
    return text + "COMMIT\n"


def nft_inventory(live):
    """Structural iptables-nft accounting fixture for the same per-chain rule view.

    Rules contain native match/verdict and xt compatibility expressions. Packet
    matching is read from iptables-save; tests also inject incompatible statements.
    This is not a claim of a captured live kernel ruleset.
    """
    import firewall
    objects = [{"metainfo": {"version": "1.0.9", "json_schema_version": 1}}]
    for version, text in live.items():
        family = "ip6" if version == 6 else "ip"
        chains, policies = firewall.parse_rules(text)
        objects.append({"table": {"family": family, "name": "filter", "handle": 1}})
        for name, rules in chains.items():
            chain = {"family": family, "table": "filter", "name": name, "handle": len(objects)}
            if name in ("INPUT", "FORWARD", "OUTPUT"):
                chain.update(type="filter", hook=name.lower(), prio=0, policy=policies[name].lower())
            objects.append({"chain": chain})
            for rule in rules:
                expressions = []
                def argument(name):
                    return rule[rule.index(name) + 1] if name in rule else None
                protocol = argument("-p")
                if protocol:
                    expressions.append({"match": {"op": "==", "left": {"meta": {"key": "l4proto"}}, "right": protocol}})
                for flag, key in (("-i", "iifname"), ("-o", "oifname")):
                    if argument(flag):
                        expressions.append({"match": {"op": "==", "left": {"meta": {"key": key}}, "right": argument(flag)}})
                for flag, field in (("--dport", "dport"), ("--sport", "sport")):
                    if argument(flag):
                        expressions.append({"match": {"op": "==", "left": {"payload": {"protocol": protocol, "field": field}}, "right": int(argument(flag))}})
                for index, token in enumerate(rule[:-1]):
                    if token == "-m" and rule[index + 1] not in ("tcp", "udp"):
                        expressions.append({"xt": {"type": "match", "name": rule[index + 1]}})
                expressions.append({"counter": {"packets": 0, "bytes": 0}})
                target = argument("-j")
                if target in ("ACCEPT", "DROP", "RETURN"):
                    expressions.append({target.lower(): None})
                elif target == "REJECT":
                    expressions.append({"reject": {"type": "icmpx", "expr": "port-unreachable"}})
                elif target == "LOG":
                    expressions.append({"log": {"prefix": argument("--log-prefix") or ""}})
                else:
                    expressions.append({"jump": {"target": target}})
                objects.append({"rule": {"family": family, "table": "filter", "chain": name,
                                          "handle": len(objects), "expr": expressions}})
    return {"nftables": objects}
