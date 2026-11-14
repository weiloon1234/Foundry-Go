"""Rehearse an application's exact acceptance archive on a disposable host."""
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import secrets
import shutil
import sys
import tarfile
import tempfile
import tomllib

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import (DeployError, MAX_JSON, canonical_digest, check_path, digest, identity,
                       read_json, require, slug, validate_project)
from topology import parse_profile, parse_topology, validate_topology
from containers import ASSETS, S3_IMAGE, Resources, boot_host, create_ca, create_host, run

DOCUMENTATION = tuple(ipaddress.ip_network(value) for value in
                      ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"))
WORK = "/var/lib/foundry-rehearsal"


def acceptance_profile(project, profile, topology):
    validate_project(project)
    parsed = parse_profile(profile, parse_topology(topology), project)
    require(profile["runtime_environment"] == "testing" and profile["environment"].startswith("acceptance")
            and profile["domain"].endswith(".acceptance.invalid") and parsed.host_id is not None,
            "rehearsal_profile", "Build an explicit testing/acceptance*/.acceptance.invalid profile with a host UUID.")
    require(not profile["edge_origin_lockdown"], "rehearsal_profile", "The isolated acceptance profile must disable Cloudflare origin lockdown.")
    require(all(binding.hostname.endswith(".acceptance.invalid") for _, binding in parsed.listeners),
            "rehearsal_profile", "Every rehearsal listener hostname must end in .acceptance.invalid.")
    return parsed


def inputs(root, environment, archive):
    require(isinstance(environment, str) and slug(environment), "rehearsal_profile", "Select ENVIRONMENT explicitly.")
    project = read_json(check_path(root / "deploy/project.json", "file"))
    profile = read_json(check_path(root / "deploy/environments" / (environment + ".json"), "file"))
    require(profile.get("environment") == environment, "rehearsal_profile", "Profile name differs from its filename.")
    topology = parse_topology(read_json(check_path(root / "deploy/topology.json", "file")))
    acceptance_profile(project, profile, topology.projection())
    if archive is None:
        return project, profile, topology, None, None
    from release import unpack_archive
    archive = check_path(Path(archive).absolute(), "file")
    checksum = digest(archive)
    entry = {**identity(project, profile), "profile": profile, "topology": validate_topology(topology.projection())}
    manifest = unpack_archive(None, archive, None, entry, checksum)
    with tarfile.open(archive, "r:gz") as bundle:
        member = bundle.getmember("config/deploy.toml")
        require(member.size <= MAX_JSON, "rehearsal_config", "Deployment configuration exceeds its bound.")
        content = bundle.extractfile(member).read()
    require(hashlib.sha256(content).hexdigest() == manifest["files"]["config/deploy.toml"],
            "rehearsal_config", "Deployment configuration differs from its inventory.")
    return project, profile, topology, manifest, tomllib.loads(content.decode())


def peer_address(subnet, cidrs, used, allowed):
    """Choose from intervals, without scanning an IPv6 address space."""
    ranges = sorted((max(int(subnet.network_address) + 1, int(c.network_address)),
                     min(int(subnet.broadcast_address) - 1, int(c.broadcast_address)))
                    for c in cidrs if c.version == subnet.version and c.overlaps(subnet))
    if allowed:
        candidates = ranges
    else:
        candidates, start = [], int(subnet.network_address) + 1
        for left, right in ranges:
            if start < left:
                candidates.append((start, left - 1))
            start = max(start, right + 1)
        candidates.append((start, int(subnet.broadcast_address) - 1))
    for left, right in candidates:
        while left <= right and str(ipaddress.ip_address(left)) in used:
            left += 1
        if left <= right:
            return str(ipaddress.ip_address(left))
    return None


def network_plan(topology, profile):
    parsed = parse_profile(profile, topology)
    addresses = {address for _, binding in parsed.listeners for address in binding.public_addresses}
    require(all(any(ipaddress.ip_address(a) in n for n in DOCUMENTATION if n.version == ipaddress.ip_address(a).version)
                for a in addresses), "rehearsal_network", "Public test listeners require documentation-range addresses.")
    by_family = {family: sorted(a for a in addresses if ipaddress.ip_address(a).version == family) for family in (4, 6)}
    require(all(len(a) <= 1 for a in by_family.values()), "rehearsal_network", "Rehearsal supports one host address per IP family.")
    hosts = [by_family[4][0] if by_family[4] else "192.0.2.10", by_family[6][0] if by_family[6] else "2001:db8::10"]
    subnets = [ipaddress.ip_network(a + ("/24" if i == 0 else "/64"), strict=False) for i, a in enumerate(hosts)]
    used = set(hosts)
    infrastructure = {}
    for role in ("gateway", "dns", "s3"):
        infrastructure[role] = []
        for subnet in subnets:
            address = peer_address(subnet, [subnet], used, True)
            require(address is not None, "rehearsal_network", "No disposable infrastructure address is available.")
            used.add(address)
            infrastructure[role].append(address)
    roles = {role.common.name: role for role in topology.roles}
    probes, records = [], {profile["domain"]: hosts}
    for name, binding in parsed.listeners:
        records[binding.hostname] = list(binding.public_addresses)
        role, listener = name.split(".")
        tls = dict(roles[role].listeners)[listener].tls
        cidrs = [ipaddress.ip_network(c) for c in binding.source_cidrs]
        for address in binding.public_addresses:
            subnet = subnets[0 if ipaddress.ip_address(address).version == 4 else 1]
            for port in binding.ports:
                probes.append({"listener": name, "address": address, "port": port, "hostname": binding.hostname,
                    "tls": tls, "allowed_peer": peer_address(subnet, cidrs, used, True),
                    "denied_peer": peer_address(subnet, cidrs, used, False)})
    return {"hosts": hosts, "subnets": [str(n) for n in subnets], **infrastructure, "records": records, "probes": probes}


def test_bindings(path, environment, config, token):
    value = {"environment": environment, "configuration": {}, "storage_disks": []}
    if path is not None:
        path = check_path(Path(path).absolute(), "file")
        require(path.is_file() and path.stat().st_mode & 0o077 == 0,
                "rehearsal_bindings", "Test binding files must be private (0600).")
        value = read_json(path)
        require(isinstance(value, dict) and set(value) == {"environment", "configuration", "storage_disks"}
                and value["environment"] == environment, "rehearsal_bindings", "Test bindings must name the selected acceptance environment.")
    require(isinstance(value["configuration"], dict) and all(isinstance(k, str) and isinstance(v, str)
            and not k.endswith("_FILE") for k, v in value["configuration"].items()),
            "rehearsal_bindings", "Test configuration accepts literal strings, without host file references.")
    names = value["storage_disks"]
    require(isinstance(names, list) and all(isinstance(n, str) for n in names) and len(names) == len(set(names)),
            "rehearsal_bindings", "List each test storage binding once.")
    disks = config.get("app", {}).get("services", {}).get("storage", {}).get("disks", {})
    external = {name for name, disk in disks.items() if disk.get("driver") in ("r2", "s3", "s3_compatible")}
    require(set(names) == external, "rehearsal_bindings", "Explicitly bind every external storage disk to the test object store.")
    access, secret = "rehearsal-" + token, secrets.token_hex(32)
    settings = {"backup_s3": {"endpoint": "https://s3:9000", "region": "us-east-1", "bucket": "backup-" + token,
                             "access_key_id": access, "secret_access_key": secret}}
    if value["configuration"]:
        settings["configuration"] = value["configuration"]
    buckets = []
    if names:
        selected = copy.deepcopy(disks)
        for name in names:
            disk = selected[name]
            require(disk["driver"] in ("r2", "s3_compatible"), "rehearsal_bindings", "This storage driver has no compatible test binding.")
            bucket = "app-" + token + "-" + str(len(buckets))
            buckets.append(bucket)
            disk["driver"] = "s3_compatible"
            disk.setdefault("cloud", {}).update(endpoint="https://s3:9000", region="us-east-1", bucket=bucket,
                credentials="rehearsal", path_style=True, compatible={"requires_nfc_keys": True,
                "conditional_create": True, "conditional_replace": True, "single_request_conditions": True})
        settings["app_storage"] = {"credentials": {"rehearsal": {"mode": "static", "access_key": access, "secret_key": secret}},
                                   "disks": selected}
    return settings, buckets


def phase(host, name, token):
    result = run(["docker", "exec", "-e", "FOUNDRY_DISPOSABLE_HOST=1", "-e", "FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1",
        "-e", "FOUNDRY_NGINX_EDGE_ACCEPTANCE=1", "-e", "FOUNDRY_REHEARSAL_RUN_ID=" + token,
        host, "python3", "-B", "/verification/deploy/local/rehearsal_host.py", name], timeout=1900, check=False)
    try:
        value = json.loads(result.stdout)
    except ValueError:
        value = {"ok": False, "error": "rehearsal_phase_unavailable"}
    if result.returncode:
        value["ok"] = False
    return value


def public_checks(resources, host_image, network, platform, certs, tools, plan, opened):
    results = []
    for probe in plan["probes"]:
        for kind in ("allowed", "denied"):
            peer = probe[kind + "_peer"]
            expected_open = opened and kind == "allowed"
            row = {"listener": probe["listener"], "address": probe["address"], "port": probe["port"],
                   "source_policy": kind, "expected_open": expected_open}
            if peer is None:
                results.append({**row, "status": "unavailable", "reason": "No matching source address in the isolated subnet"})
                continue
            name = resources.reserve("container", "peer-" + str(len(resources.items)))
            result = run(["docker", "run", "--rm", "--name", name, "--label", resources.label,
                "--platform", platform, "--network", network, "--ip6" if ":" in peer else "--ip", peer,
                "--cap-drop=ALL", "--security-opt=no-new-privileges", "--memory=128m", "--cpus=0.5",
                "--mount", f"type=bind,source={certs / 'ca.crt'},target=/ca.crt,readonly",
                "--mount", f"type=bind,source={tools / 'local/rehearsal_probe.py'},target=/probe.py,readonly",
                host_image, "python3", "-B", "/probe.py", probe["address"], str(probe["port"]), probe["hostname"],
                probe["tls"], "open" if expected_open else "closed"], timeout=30, check=False)
            try:
                evidence = json.loads(result.stdout)
            except ValueError:
                evidence = {"ok": False, "error": "peer_probe_unavailable"}
            results.append({**row, "status": "passed" if result.returncode == 0 and evidence.get("ok") else "failed", **evidence})
    return results


def rehearse(root, environment, archive, *, ubuntu="24.04", output=None, bindings=None, keep_failed=False,
             cpus=2, memory_gib=4, builder=None):
    require(ubuntu in ("24.04", "26.04") and type(cpus) is int and 1 <= cpus <= 32
            and type(memory_gib) is int and 2 <= memory_gib <= 64, "rehearsal_limits", "Unsupported Ubuntu or resource limits.")
    project, profile, topology, manifest, config = inputs(root, environment, archive)
    plan = network_plan(topology, profile)
    if archive is None:
        require(builder is not None, "rehearsal_archive", "Supply --archive or the application builder.")
        archive = builder(project, profile)
        project, profile, topology, manifest, config = inputs(root, environment, archive)
    archive = Path(archive).absolute()
    # Validate all private inputs before contacting Docker or creating resources.
    settings, buckets = test_bindings(bindings, environment, config, secrets.token_hex(8))
    architecture = run(["docker", "info", "--format", "{{.Architecture}}"], timeout=30).stdout.strip()
    require({"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(architecture)
            == profile["architecture"], "rehearsal_architecture", "Use a native Docker engine matching the archive architecture.")
    directory = Path(output).absolute() if output else Path(tempfile.mkdtemp(prefix="foundry-rehearsal-")).resolve()
    if output:
        directory.mkdir(mode=0o700, parents=False)
    directory.chmod(0o700)
    resources = Resources(directory)
    private = directory / "private"
    private.mkdir(mode=0o700)
    evidence = {"run_id": resources.token, "status": "failed", "archive_sha256": digest(archive), "environment": environment,
        "profile_sha256": canonical_digest(profile), "topology_sha256": topology.sha256, "ubuntu": ubuntu,
        "architecture": profile["architecture"], "cpus": cpus, "memory_gib": memory_gib, "network": plan,
        "phases": [], "external_checks": {"production_dns_acme": "unavailable", "cloud_firewalls": "unavailable",
        "global_dns_cutover": "manual", "public_udp": "unavailable"}}
    platform, failed = "linux/" + profile["architecture"], True
    try:
        from upgrades import tool_bundle
        source, files, bundle_digest = tool_bundle(ASSETS.parent)
        tools = private / "tools"
        for file in files:
            target = tools / file.relative_to(source)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(file, target)
        require(tool_bundle(tools)[2] == bundle_digest, "rehearsal_tools", "Tool source changed while freezing the run.")
        evidence["tools_sha256"] = bundle_digest
        base = resources.reserve("image", "base")
        image = resources.reserve("image", "host")
        for name, dockerfile, argument in ((base, "host.Dockerfile", "UBUNTU_VERSION=" + ubuntu),
                                          (image, "acceptance.Dockerfile", "BASE_IMAGE=" + base)):
            run(["docker", "build", "--platform", platform, "--label", resources.label, "-f", tools / "local" / dockerfile,
                 "--build-arg", argument, "-t", name, tools / "local"], timeout=1800)
        evidence["image_id"] = run(["docker", "image", "inspect", image, "--format", "{{.Id}}"], timeout=30).stdout.strip()
        evidence["s3_image"] = S3_IMAGE
        network = resources.reserve("network", "runtime")
        run(["docker", "network", "create", "--label", resources.label, "--internal", "--ipv6",
             "--subnet", plan["subnets"][0], "--gateway", plan["gateway"][0],
             "--subnet", plan["subnets"][1], "--gateway", plan["gateway"][1], network])
        provision = resources.reserve("network", "provision")
        run(["docker", "network", "create", "--label", resources.label, provision])
        certs = private / "tls"
        create_ca(certs)
        dns = resources.reserve("container", "dns")
        records = {**plan["records"], "s3": plan["s3"]}
        run(["docker", "run", "-d", "--name", dns, "--label", resources.label, "--network", network,
            "--ip", plan["dns"][0], "--ip6", plan["dns"][1], "--memory=128m", "--cpus=0.5", image,
            "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts", "--bind-interfaces",
            *["--host-record=" + name + "," + ",".join(addresses) for name, addresses in records.items()]])
        volume = resources.reserve("volume", "storage")
        run(["docker", "volume", "create", "--label", resources.label, volume])
        store = resources.reserve("container", "s3")
        environment = {**os.environ, "ROOT_ACCESS_KEY_ID": settings["backup_s3"]["access_key_id"],
                       "ROOT_SECRET_ACCESS_KEY": settings["backup_s3"]["secret_access_key"]}
        run(["docker", "create", "--platform", platform, "--name", store, "--label", resources.label,
            "--network", network, "--network-alias", "s3", "--ip", plan["s3"][0], "--ip6", plan["s3"][1],
            "--user", "0:0", "--memory=512m", "--cpus=1", "-e", "ROOT_ACCESS_KEY_ID", "-e", "ROOT_SECRET_ACCESS_KEY",
            "--mount", f"type=volume,source={volume},target=/data", S3_IMAGE,
            "--cert", "/certs/public.crt", "--key", "/certs/private.key", "--port", ":9000",
            "--region", "us-east-1", "posix", "/data"], env=environment)
        server_certs = private / "s3-tls"
        server_certs.mkdir(mode=0o700)
        for file in ("public.crt", "private.key"):
            shutil.copyfile(certs / file, server_certs / file)
        run(["docker", "cp", server_certs, store + ":/certs"])
        run(["docker", "start", store])
        host = resources.reserve("container", "host")
        create_host(host, image, network, platform, [f"type=bind,source={tools},target=/verification/deploy,readonly"],
                    cpus=cpus, memory_gib=memory_gib,
                    extra=["--label", resources.label, "--ip", plan["hosts"][0], "--ip6", plan["hosts"][1]])
        run(["docker", "network", "connect", provision, host])
        boot_host(host, certs)
        run(["docker", "exec", host, "mkdir", "-m", "700", WORK])
        context = {"run_id": resources.token, "project": project, "profile": profile, "topology": topology.projection(),
                   "archive_sha256": evidence["archive_sha256"], "storage_buckets": buckets}
        for name, value in (("context.json", context), ("secrets.json", settings)):
            path = private / name
            path.write_text(json.dumps(value))
            path.chmod(0o600)
            run(["docker", "cp", path, host + ":" + WORK + "/" + name])
        run(["docker", "cp", archive, host + ":" + WORK + "/app.tar.gz"], timeout=300)
        for name in ("prepare", "bootstrap-plan", "bootstrap-apply", "isolate", "closed-probes", "activate", "observe",
                     "open-probes", "stop", "stopped", "stopped-probes", "restart", "observe", "restarted-probes", "stop"):
            if name == "isolate":
                run(["docker", "network", "disconnect", provision, host])
                run(["docker", "exec", "-i", host, "sh", "-c", "cat > /etc/resolv.conf"],
                    input="nameserver " + plan["dns"][0] + "\noptions timeout:1 attempts:1\n")
                result = {"ok": True, "runtime_network": "internal", "dns": "local-only"}
            elif name.endswith("probes"):
                probes = public_checks(resources, image, network, platform, certs, tools, plan,
                                       name in ("open-probes", "restarted-probes"))
                result = {"ok": all(p["status"] != "failed" for p in probes), "probes": probes}
            else:
                result = phase(host, name, resources.token)
            evidence["phases"].append({"phase": name, **result})
            (directory / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
            require(result.get("ok", True), "rehearsal_phase", "Rehearsal phase refused: " + name + "; see evidence.json.")
        failed = False
        evidence["status"] = "passed"
    except BaseException as error:
        evidence["error"] = error.code if isinstance(error, DeployError) else "rehearsal_failed"
        raise DeployError("rehearsal_failed", "Rehearsal stopped; see the run's evidence.json.") from None
    finally:
        if failed and keep_failed:
            evidence["retained"] = True
        else:
            cleanup = resources.cleanup()
            evidence["cleanup_failures"] = cleanup
            if not cleanup:
                shutil.rmtree(private)
            else:
                evidence["status"] = "failed"
        (directory / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
        print("Rehearsal run " + resources.token + ": " + str(directory), file=sys.stderr)
    require(evidence["status"] == "passed", "rehearsal_cleanup", "Rehearsal cleanup is incomplete; inspect resources.json.")
    return directory
