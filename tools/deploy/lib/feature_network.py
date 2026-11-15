"""Network adapter boundary for scoped and whole-instance lifecycle operations.

No caller may treat ingress withdrawal as complete application closure.
"""
import importlib
from dataclasses import replace
import hashlib
from pathlib import Path
import re

from contracts import AllocatedEndpoint, ClosureEvidence, ClosureStage, DeployError, contract_digest, require

BOOT_UNIT = "foundry-edge-boot.service"
BOOT_PATH = "/etc/systemd/system/" + BOOT_UNIT


def boot_command(bundle):
    import services
    return ["/usr/bin/python3", "-I", services.RUNTIME_ENTRY, "network-boot", bundle]


def boot_unit(bundle):
    import services
    command = services.command_line(boot_command(bundle))
    content = ("# Foundry feature network boot coordinator\n[Unit]\nDescription=Close feature routes before nginx\n"
               "After=ufw.service\nBefore=nginx.service\n[Service]\nType=oneshot\nUser=root\n"
               "RemainAfterExit=yes\nExecStart=" + command + "\nTimeoutStartSec=120\n")
    return content


def boot_files(tools_dir, bundle):
    dropin = (Path(tools_dir) / "server/templates/foundry-edge-boot.conf").read_text().replace("@BOOT_UNIT@", BOOT_UNIT)
    return {BOOT_PATH: boot_unit(bundle),
            "/etc/systemd/system/nginx.service.d/foundry-edge-boot.conf": dropin}


def boot_pin(host):
    """Read the exact owned coordinator and its verified immutable bundle."""
    import admission
    import upgrades
    from contracts import HOST_PATHS
    path = admission.protected_path(host, host.paths.p(BOOT_PATH), "file")
    if not path.exists():
        return None
    pattern = re.escape(boot_unit("@BUNDLE@")).replace(re.escape("@BUNDLE@"), r"([a-f0-9]{64})")
    matched = re.fullmatch(pattern, path.read_text())
    require(matched is not None, "nginx_boot_ownership", "The feature boot coordinator differs from its reviewed owner.")
    bundle = matched[1]
    pinned = upgrades._installed(host, {"bundle_sha256": bundle,
                                "active_path": HOST_PATHS["tools"] + "/tools/" + bundle})
    for name, content in boot_files(pinned, bundle).items():
        installed = admission.protected_path(host, host.paths.p(name), "file")
        require(not installed.exists() or installed.read_text() == content,
                "nginx_boot_ownership", "The feature boot coordinator differs from its reviewed owner.")
    return bundle, pinned


def checked_boot_files(host, tools_dir, bundle):
    """A coordinator must use the selected tools' registry and profile readers."""
    import admission
    import services
    from operations import runtime_compatible
    pin = boot_pin(host)
    if pin:
        require(pin[0] == bundle, "boot_coordinator_upgrade_required",
                "Advance the boot coordinator through a tools upgrade before changing application state.")
        runtime = admission.protected_path(host, host.paths.p(services.RUNTIME_ENTRY), "file")
        require(runtime.exists() and runtime_compatible(runtime.read_bytes(), (pin[1] / "server/runtime_v1.py").read_bytes()),
                "runtime_bridge_required", "The boot coordinator requires its compatible fixed runtime.")
    return boot_files(tools_dir, bundle)


def boot_migration(host, tools_dir, bundle):
    pin = boot_pin(host)
    if not pin or pin[0] == bundle:
        return None
    # Only the bundle pin changes here. A new coordinator/drop-in format needs
    # its own migration; never replace an administrator's configuration.
    require(all(name == BOOT_PATH or content == boot_files(pin[1], pin[0])[name]
                for name, content in boot_files(tools_dir, bundle).items()),
            "nginx_boot_ownership", "The boot coordinator policy requires a separate migration.")
    return {"path": BOOT_PATH, "from_bundle": pin[0], "to_bundle": bundle}


def advance_boot(host, tools_dir, bundle):
    """Resume the upgrade's atomic pin publication, reload and loaded-unit proof."""
    require(host.host_lock_fd is not None, "tools_lock_required", "Boot coordinator migration requires the host lock.")
    if not boot_pin(host):
        return
    if boot_migration(host, tools_dir, bundle):
        host.owned_write(host.paths.p(BOOT_PATH), boot_files(tools_dir, bundle)[BOOT_PATH])
    checked_boot_files(host, tools_dir, bundle)
    # Repeat after a crash even when the file already has the target pin.
    host.run(["systemctl", "daemon-reload"])
    loaded = host.run(["systemctl", "show", BOOT_UNIT,
        "--property=LoadState,FragmentPath,DropInPaths,NeedDaemonReload,ExecStart", "--no-pager"])
    fields = dict(line.split("=", 1) for line in loaded.stdout.splitlines() if "=" in line)
    require(all(fields.get(key) == value for key, value in
                {"LoadState": "loaded", "FragmentPath": BOOT_PATH, "DropInPaths": "", "NeedDaemonReload": "no"}.items())
            and re.findall(r"argv\[\]=(.*?) ;", fields.get("ExecStart", "")) == [" ".join(boot_command(bundle))],
            "boot_coordinator_unverified", "The loaded boot coordinator does not match its target bundle.")


def check_registry_writer(host, registry):
    """Refuse new boot-visible state until the selected bundle owns the coordinator."""
    from contracts import read_json
    path = host.paths.p(BOOT_PATH)
    if not path.exists() and not path.is_symlink():
        return  # Bootstrap has not installed a coordinator yet.
    if registry["tools"].get("upgrade_operation") and host.paths.registry.exists():
        previous = read_json(host.paths.registry)
        metadata = {"revision", "updated_at", "tools"}
        if ({key: value for key, value in registry.items() if key not in metadata}
                == {key: value for key, value in previous.items() if key not in metadata}):
            return  # The upgrade intent may precede pin publication; application state cannot.
    source = host.paths.p(registry["tools"]["active_path"])
    checked_boot_files(host, source, registry["tools"]["bundle_sha256"])
    from upgrades import feature_generation, protocol
    from topology import parse_topology
    generation = protocol(source)["generation"]
    for entry in registry["instances"].values():
        if entry.get("topology", {}).get("schema") == 2:
            require(feature_generation(parse_topology(entry["topology"], registered=True), entry) <= generation,
                    "boot_coordinator_upgrade_required", "The boot coordinator cannot read the requested capability generation.")


def bootstrap_ready(host, entry, tools_dir, bundle):
    import admission
    import edge
    from contracts import PREFIX
    from provision import _template
    try:
        boot = checked_boot_files(host, tools_dir, bundle)
    except DeployError:
        return False
    expected = {**boot, edge.STREAM_INCLUDE: (Path(tools_dir) / "server/templates/nginx-stream.conf").read_text()}
    for name, text in expected.items():
        path = admission.protected_path(host, host.paths.p(name), "file")
        if not path.exists() or path.read_text() != text:
            return False
    site = host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{entry['name']}.conf")
    enabled = host.paths.p(f"/etc/nginx/sites-enabled/{site.name}")
    rendered = _template(tools_dir, "nginx-https.conf", entry)
    accepted = {closed_http(entry, rendered)}
    if "deployment_selection" in entry:
        accepted.add(edge.gate_http(entry["profile"]["instance_id"], rendered))
    return site.is_file() and site.read_text() in accepted and enabled.is_symlink() and enabled.resolve() == site


def package_topology(topology):
    """The platform's stream renderer needs its module even for HTTP-only features.

    Pass a typed planning copy to the package owner; app declarations stay intact.
    """
    return replace(topology, roles=tuple(replace(role, common=replace(role.common,
        packages=tuple(sorted(set(role.common.packages) | {"libnginx-mod-stream"}))))
        if role.common.name == "http" else role for role in topology.roles))


def plan(host, topology, profile, package_plan, *, entry=None):
    import edge
    import firewall
    import listeners
    from contracts import check_path, digest
    listeners.public_policies(profile, topology)
    import recovery_rehearsal
    claims = () if entry is not None and recovery_rehearsal.active(entry) else listeners.public_claims(profile)
    result = {"firewall": None, "persistent_projection": {}}
    if claims:
        selected = entry is not None and "deployment_selection" in entry
        if selected:
            from feature_lifecycle import load
            from contracts import AdmissionDisposition, record_document
            _, selection, inventory, _ = load(host, entry)
            require(record_document(selection) == entry["deployment_selection"]
                    and selection.admission.disposition is AdmissionDisposition.COMMITTED
                    and inventory.topology == topology and inventory.profile == profile,
                    "network_selection", "Bootstrap inspection requires the committed selected declarations.")
            import feature_maintenance
            import services
            held = feature_maintenance.active_holds(host, entry, inventory)
            exposed = set(services.enabled_units(host, inventory.units)) - held
            opened = tuple(c for c in claims if c.identity in exposed)
            closed = tuple(c for c in claims if c.identity not in exposed)
            observation = firewall.observe(host, profile.instance_id, closed or opened, integrated=True,
                                           bindings=inventory.profile.listeners, opened=not closed,
                                           retained_claims=opened if closed else ())
        else:
            observation = firewall.observe(host, profile.instance_id, claims, integrated=False)
            for family, name in ((4, "/etc/ufw/before.rules"), (6, "/etc/ufw/before6.rules")):
                result["persistent_projection"][name] = firewall.persistent_projection(
                    check_path(host.paths.p(name), "file").read_text(), profile.instance_id, family=family)
        result["firewall"] = observation.sha256
    main = check_path(host.paths.p("/etc/nginx/nginx.conf"), "file")
    if main.exists():
        result["nginx_sha256"] = digest(main)
        require(edge._top_level_include(main.read_text(), "/etc/nginx/modules-enabled/*.conf"),
                "nginx_stream_integration", "Review the top-level nginx modules include before feature bootstrap.")
    else:
        require("nginx-common" in package_plan.get("transaction", {}), "nginx_stream_integration",
                "Fresh nginx configuration must come from the reviewed nginx-common package transaction.")
        result["nginx_sha256"] = None
    return result


def prepare(host, entry, review, tools_dir, bundle):
    """Apply only the network prerequisites included in the confirmed bootstrap plan."""
    import edge
    import firewall
    import listeners
    import operations
    from contracts import check_path, digest
    from topology import parse_profile, parse_topology
    topology = parse_topology(entry["topology"], registered=True)
    profile = parse_profile(entry["profile"], topology)
    import recovery_rehearsal
    claims = () if recovery_rehearsal.active(entry) else listeners.public_claims(profile)
    boot = checked_boot_files(host, tools_dir, bundle)
    operations.check_runtime(host, tools_dir)
    main = check_path(host.paths.p("/etc/nginx/nginx.conf"), "file")
    checksum = review["nginx_sha256"]
    if checksum is None:
        # The plan approved a new authenticated distro package, not an arbitrary
        # config that appeared after review. Require its pristine conffile bytes.
        output = host.run(["dpkg-query", "-W", "-f=${Conffiles}", "nginx-common"]).stdout
        expected = re.findall(r"^ /etc/nginx/nginx\.conf ([a-f0-9]{32})$", output, re.M)
        require(expected == [hashlib.md5(main.read_bytes()).hexdigest()], "nginx_stream_integration",
                "New nginx configuration differs from the reviewed package's pristine conffile.")
        checksum = digest(main)
    edge.check_stream_include(host, reviewed_nginx_sha256=checksum)
    edge.check_boot(host, profile.instance_id)
    if claims:
        firewall.install_closed(host, profile.instance_id, claims, reviewed_sha256=review["firewall"])
    edge.install_stream_include(host, reviewed_nginx_sha256=checksum)
    edge.prepare_boot(host, profile.instance_id)
    operations.install_runtime(host, tools_dir)
    for name, content in boot.items():
        host.owned_write(host.paths.p(name), content)
    host.run(["systemctl", "daemon-reload"])


def closed_http(entry, rendered):
    import edge
    # A bootstrap site carries no backend reference before its first allocation.
    return re.sub(r"proxy_pass http://127\.0\.0\.1:[0-9]+;", "return 503;",
                  edge.gate_http(entry["profile"]["instance_id"], rendered))


def prepare_boot(host):
    """Clear runtime routes before nginx without replaying pending operations."""
    import edge
    registry = host.read_registry()
    for entry in registry["instances"].values():
        if entry.get("topology", {}).get("schema") == 2 or "deployment_selection" in entry:
            edge.prepare_boot(host, entry["profile"]["instance_id"])


def capability():
    try:
        owner = importlib.import_module("listeners")
    except ModuleNotFoundError as error:
        if error.name != "listeners":
            raise
        owner = None
    require(owner is not None and all(callable(getattr(owner, name, None)) for name in
            ("feature_allocate", "feature_withdraw", "feature_close", "feature_open")),
            "capability_unavailable", "Public activation requires the B24 lane C preview network integration release.")
    return owner


def allocate(host, entry, topology, profile, operation_id, *, retained=None):
    """Return (tuple[AllocatedEndpoint], nginx projection SHA256), without effects."""
    endpoints, projection = capability().feature_allocate(host, entry, topology, profile, operation_id,
                                                         **({"retained": retained} if retained else {}))
    from dataclasses import asdict
    endpoints = tuple(AllocatedEndpoint.parse(asdict(endpoint)) for endpoint in endpoints)
    return endpoints, contract_digest(projection)


def close(host, entry, transition, resolution, units):
    """Observe complete application closure for the exact transition's units/addresses."""
    from contracts import record_document, UnitIdentity
    evidence = capability().feature_close(host, entry, transition, resolution, units)
    evidence = ClosureEvidence.parse(record_document(evidence))
    require(evidence.stage is ClosureStage.APPLICATION_CLOSED and evidence.operation_id == transition.operation_id
            and evidence.candidate_resolution_id == resolution.id
            and set(transition.affected_units) == set(evidence.units)
            and {e for e in resolution.endpoints if UnitIdentity(transition.instance_id, e.role, e.ordinal) in transition.affected_units} <= set(evidence.endpoints),
            "admission_closed_required", "Network owner did not prove complete closure for this transition.")
    return evidence


def withdraw(host, entry, transition, resolution, units):
    """Withdraw new ingress before stopping live workloads.

    The final close call must still prove complete closure after verified exit.
    An ingress-only observation never authorizes a start or completed cleanup.
    """
    return capability().feature_withdraw(host, entry, transition, resolution, units)


def open_public(host, entry, resolution, evidence, *, deadline=None):
    require(capability().feature_open(host, entry, resolution, evidence,
            **({"deadline": deadline} if deadline is not None else {})) is True,
            "public_opening_unverified", "Network owner did not verify public opening for the selected resolution.")
