#!/usr/bin/env python3
"""Real local operator functions over Docker exec instead of an SSH connection."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tarfile
import tempfile
from unittest.mock import patch

DEPLOY = Path(__file__).resolve().parents[1]
REMOTE = "/var/lib/foundry-acceptance-f4a/operator"


def prepare_fixture(source, *, bound=False):
    from acceptance_support import guard, private_json
    from acceptance_feature import feature_archive
    import acceptance_host as shared
    import acceptance_network as network
    from host import Host
    guard()
    host = Host()
    destination = Path(REMOTE)
    destination.mkdir(mode=0o700, exist_ok=True)
    if bound:
        host_id = host.read_registry()["host_id"]
    else:
        assert host.read_registry(missing=True) is None, "Operator phase requires a fresh host"
        host_id = None
        # Namespace resources live only for this disposable container's lifetime.
        network.prepare_peer(host, lambda *args: None)
        network.prepare_firewall(host)
    project, profile, topology, disks = feature_archive(source, destination / "app.tar.gz", host_id)
    if not bound:
        private_json(destination / "project.json", project)
        private_json(destination / "profile.json", profile)
        private_json(destination / "topology.json", topology.projection())
        shared.WORK.mkdir(mode=0o700, exist_ok=True)
        private = shared.bootstrap_secrets(disks)
        private["cloudflare_dns_token"] = "acceptance-synthetic-token"
        private.setdefault("configuration", {})["FOUNDRY_SERVICE_SECRET__RELAY_TEMPLATE_TOKEN"] = secrets.token_urlsafe(32)
        for name, value in private.items():
            name = "config" if name == "configuration" else name
            (destination / (name + ".secret")).write_text(value if isinstance(value, str) else json.dumps(value))
            (destination / (name + ".secret")).chmod(0o600)


def operator(host, env_file):
    spec = importlib.util.spec_from_file_location("operator_cli", DEPLOY / "local/cli.py")
    cli = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(cli)
    calls = []

    class DockerSSH(cli.SSH):
        def __init__(self, *args, **kwargs):
            self.base = ["docker", "exec", "--env-file", str(env_file), "-i", host, "sh", "-c"]

        def close(self):
            pass

        def request(self, action, request, tools=None):
            calls.append((action, tools is not None))
            return super().request(action, request, tools)

    with tempfile.TemporaryDirectory(prefix="foundry-operator-") as temporary:
        root = Path(temporary).resolve()
        cli.ROOT = root
        subprocess.run(["docker", "cp", host + ":" + REMOTE + "/.", str(root)], check=True, timeout=120)
        project = cli.read_json(root / "project.json")
        profile = cli.read_json(root / "profile.json")
        assert profile["schema"] == 3 and profile["host"]["host_id"] is None
        assert cli.read_json(root / "topology.json")["schema"] == 2
        (root / "deploy").mkdir()
        (root / "topology.json").rename(root / "deploy/topology.json")
        environment = {"CONFIRM": "yes", "ROLE": "relay"}
        environment.update({"FOUNDRY_SECRET_" + p.stem.upper() + "_FILE": str(p) for p in root.glob("*.secret")})
        with patch.object(cli, "SSH", DockerSSH), patch.dict(os.environ, environment):
            for action in ("inspect", "prepare"):
                with patch.dict(os.environ, ACTION=action):
                    cli.bootstrap(project, profile, {}, fresh=True)
            assert calls[:4] == [(a, True) for a in ("inspect", "inspect", "bootstrap-plan", "bootstrap-apply")]
            assert profile["host"]["host_id"] is not None
            # Repackage the already-built acceptance binary with the committed binding,
            # exactly as the existing direct feature acceptance fixture does.
            subprocess.run(["docker", "exec", "--env-file", str(env_file), host, "python3", "-B",
                            "/verification/deploy/tests/acceptance_operator.py", "--prepare-fixture", "--bound"],
                           check=True, timeout=180)
            subprocess.run(["docker", "cp", host + ":" + REMOTE + "/app.tar.gz", str(root / "app.tar.gz")],
                           check=True, timeout=120)
            with tarfile.open(root / "app.tar.gz") as archive:
                manifest = json.load(archive.extractfile("manifest.json"))
            release = root / "dist/releases" / project["id"] / profile["instance_id"] / manifest["version"]
            release.mkdir(parents=True)
            (root / "app.tar.gz").rename(release / "app.tar.gz")
            cli.atomic_json(release / "manifest.json", manifest)
            with patch.dict(os.environ, ACTION="deploy", RELEASE=manifest["version"]):
                cli.deploy(project, profile, {})
            assert [a for a, _ in calls[4:]] == ["sync", "status", "hello", "release-plan", "activate", "status"]
            for action in ("status", "logs", "report"):
                with patch.dict(os.environ, {"ACTION": action, **({"LINES": "10"} if action == "logs" else {})}):
                    cli.deploy(project, profile, {})
            with patch.dict(os.environ, ACTION="inspect"):
                cli.bootstrap(project, profile, {}, fresh=False)
            remote = DockerSSH(profile, {})
            status = remote.request("status", {"project": project, "profile": profile})
            assert status["state"]["status"] == "healthy"
            assert status["state"]["current"]["version"] == manifest["version"]
            assert "logs" in remote.request("logs", {"project": project, "profile": profile, "role": "relay", "lines": 10})
    print("PASS operator bootstrap inspect/prepare, reviewed deploy, status, logs and report")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host")
    parser.add_argument("--env-file", type=Path)
    parser.add_argument("--prepare-fixture", action="store_true")
    parser.add_argument("--bound", action="store_true")
    args = parser.parse_args()
    if args.prepare_fixture:
        sys.path.insert(0, str(DEPLOY / "lib"))
        prepare_fixture(Path("/var/lib/foundry-acceptance-f4a/input.tar.gz"), bound=args.bound)
    else:
        operator(args.host, args.env_file)


if __name__ == "__main__":
    main()
