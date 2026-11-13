#!/usr/bin/env python3
"""Public build input and release packaging, shared by both target architectures."""
import gzip
from dataclasses import asdict
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import artifacts
from contracts import (ARCHIVE_SCHEMA, FEATURE_PROFILE_SCHEMA, TOPOLOGY_SCHEMA, PayloadMember,
                       ReleaseDeclaration, check_path, read_json, require)
from topology import ExecutableRole, parse_profile, parse_topology


def public_env(request, destination):
    # The browser build's public-env guard remains the authoritative allowlist.
    lines = []
    for key, value in request["profile"]["public_env"].items():
        if not re.fullmatch(r"VITE_[A-Z0-9_]+", key) or not isinstance(value, str):
            raise ValueError("invalid public build setting")
        if any(char in value for char in "\r\n\x00$"):
            raise ValueError("public build values must be literal single-line strings")
        # Literal dotenv quoting, not JSON escaping: the existing Vite guard
        # checks that every value occurs unchanged on its assignment line.
        # Double-quoted dotenv values expand literal \\n/\\r sequences. Prefer
        # the literal delimiters, falling back only when decoding stays exact.
        quote = next((delimiter for delimiter in ("'", "`", '"')
                      if delimiter not in value and (delimiter != '"' or not any(
                          escape in value for escape in ("\\n", "\\r")))), None)
        if quote is None:
            raise ValueError("public build value cannot be represented literally with dotenv quoting")
        lines.append(f"{key}={quote}{value}{quote}\n")
    Path(destination).write_text("".join(lines), encoding="utf-8")


def public_command(args):
    # Keep package inventory output off the heap until its size is checked.
    with tempfile.TemporaryFile() as output:
        subprocess.run(args, stdout=output, stderr=subprocess.DEVNULL, check=True, timeout=30)
        if output.tell() > 4 * 1024 * 1024:
            raise ValueError("build provenance command output exceeds 4 MiB")
        output.seek(0)
        value = output.read().decode("utf-8")
    if not value.strip():
        raise ValueError("build provenance command returned no output")
    return value


def collect_build_inputs(request):
    packages = public_command(["dpkg-query", "-W", "-f", "${Package} ${Version} ${Architecture}\n"])
    packages = "".join(line + "\n" for line in sorted(packages.splitlines()))
    return {"images": request["images"],
            "go": public_command(["go", "version"]).strip(),
            "node": public_command(["node", "--version"]).strip(),
            "npm": public_command(["npm", "--version"]).strip(),
            "cc": public_command([os.environ["CC"], "--version"]).splitlines()[0],
            "packages_sha256": hashlib.sha256(packages.encode("utf-8")).hexdigest()}


def prepare(request, work, stage):
    """Copy/build only explicitly declared sources from the frozen snapshot."""
    work, stage = Path(work), Path(stage)
    topology = parse_topology(read_json(check_path(work / "deploy/topology.json", "file")))
    if topology.schema == 1:
        return request
    profile = parse_profile(request["profile"], topology)
    platform = "linux-" + profile.architecture
    declarations = {}
    for role in topology.roles:
        if isinstance(role, ExecutableRole):
            for _, config in role.configs:
                declarations[config.source] = config.source
    for name, binary in topology.application_executables:
        if binary.go_package is not None:
            destination = stage / "deploy/bin" / name
            check_path(destination, "file")
            destination.parent.mkdir(parents=True, exist_ok=True)
            require(not destination.exists(), "build_conflict", "Declared binary destination already exists.")
            environment = {**os.environ, "GOWORK": "off", "GOOS": "linux", "GOARCH": profile.architecture}
            subprocess.run(["go", "build", "-p", "1", "-trimpath", "-o", str(destination), binary.go_package],
                           cwd=work, env=environment, check=True, timeout=1200)
        else:
            path = dict(binary.platforms)[platform]
            declarations[path] = path
    for source, destination in sorted(declarations.items()):
        artifacts._safe_path(destination)
        source_path = check_path(work / source, "file")
        target = check_path(stage / destination, "file")
        require(source_path.is_file() and not target.exists(), "build_source", "Declared source is missing or overlaps staged content.")
        target.parent.mkdir(parents=True, exist_ok=True)
        with source_path.open("rb") as incoming, target.open("xb") as outgoing:
            shutil.copyfileobj(incoming, outgoing, artifacts.CHUNK)
    return {**request, "topology": json.loads(topology.canonical_json)}


def _feature_inputs(request, stage, budget):
    topology = parse_topology(request["topology"])
    profile = parse_profile(request["profile"], topology)
    require(topology.schema == TOPOLOGY_SCHEMA and profile.architecture == request["manifest"]["architecture"]
            and profile.instance_id == request["manifest"]["instance_id"]
            and request["profile"]["environment"] == request["manifest"]["environment"]
            and request["manifest"].get("topology_sha256", topology.sha256) == topology.sha256,
            "build_identity", "Build topology/profile identity differs from the frozen manifest.")
    platform = "linux-" + profile.architecture
    exports, payloads, origins = {}, [], {}
    for name, binary in topology.application_executables:
        path = "deploy/bin/" + name if binary.go_package else dict(binary.platforms)[platform]
        exports[name] = path
    for name, artifact in topology.artifacts:
        pin = dict(artifact.platforms)[platform]
        path = f"deploy/artifacts/{name}/{pin.sha256}." + ("tar.gz" if artifact.format == "tar.gz" else "bin")
        destination = check_path(stage / path, "file")
        destination.parent.mkdir(parents=True, exist_ok=True)
        # Never trust a preexisting staged payload or silently acquire a new pin.
        origins[name] = [asdict(origin) for origin in artifacts.download(pin, destination, budget=budget, budget_path=path)]
        members = artifacts.inspect_payload(artifact, platform, destination, budget)
        payloads.append({"artifact": name, "platform": platform, "path": path,
                         "sha256": pin.sha256, "members": [asdict(member) for member in members]})
    kinds = artifacts.declared_release_files(topology, exports.values(), (payload["path"] for payload in payloads))
    return topology, profile, kinds, exports, payloads, origins


class _ArchiveOutput:
    def __init__(self, output, budget):
        from release import MAX_ARCHIVE_BYTES
        self.output, self.budget, self.limit, self.size = output, budget, MAX_ARCHIVE_BYTES, 0

    def write(self, block):
        size = self.size + len(block)
        require(size <= self.limit, "build_archive", "Release exceeds its compressed size limit.")
        self.budget.grow(self.size, len(block), expanded=False)
        require(shutil.disk_usage(Path(self.output.name).parent).free >= len(block) + 4096,
                "build_archive", "Insufficient staging disk space for release archive.")
        self.size = size
        return self.output.write(block)

    def flush(self):
        self.output.flush()


def package(request, stage, output):
    stage, output = Path(stage), Path(output)
    manifest = dict(request["manifest"])
    budget = artifacts.ExpansionBudget()
    feature = "topology" in request and parse_topology(request["topology"]).schema == TOPOLOGY_SCHEMA
    require(request.get("profile", {}).get("schema") != FEATURE_PROFILE_SCHEMA or feature,
            "build_identity", "A capability profile requires its frozen topology snapshot.")
    if feature:
        topology, profile, kinds, exports, payloads, origins = _feature_inputs(request, stage, budget)
        directories = {parent.as_posix() for name in kinds for parent in Path(name).parents if str(parent) != "."}
    else:
        kinds, directories = {}, set()
    files = {}
    inventory = []
    for path in sorted(stage.rglob("*")):
        relative = path.relative_to(stage).as_posix()
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError(f"release contains a non-regular path: {relative}")
        allowed = (relative == "artisan" or relative in {"config", "config/deploy.toml", "public", "private-source-maps"}
                   or relative.startswith(("public/", "private-source-maps/")) or relative in kinds
                   or path.is_dir() and relative in directories)
        if not allowed:
            raise ValueError(f"unexpected release path: {relative}")
        if any(part.startswith(".env") or part in {".git", "node_modules"} for part in path.relative_to(stage).parts):
            raise ValueError("release contains a forbidden private path")
        if path.suffix.lower() in {".pem", ".key", ".p12", ".pfx"}:
            raise ValueError("release contains key material")
        if kinds.get(relative) != "artifact":
            budget.charge(relative, path.stat().st_size if path.is_file() else 0, directory=path.is_dir())
        if path.is_file():
            require(path.stat().st_nlink == 1, "build_inventory", "Release files cannot be hard links.")
            with path.open("rb") as content:
                files[relative] = hashlib.file_digest(content, "sha256").hexdigest()
            if feature:
                kind = artifacts.release_member_kind(relative, kinds)
                if kind == "application":
                    with path.open("rb") as content:
                        artifacts.validate_binary(content.read(64), "linux-" + profile.architecture)
                inventory.append(PayloadMember(relative, files[relative], path.stat().st_size, kind))
    if not {"artisan", "config/deploy.toml"} <= files.keys():
        raise ValueError("release lacks its binary or deployment configuration")
    manifest["files"] = files
    if feature:
        deployment = ReleaseDeclaration.parse({"schema": ARCHIVE_SCHEMA, "topology": json.loads(topology.canonical_json),
            "profile": profile.projection(), "topology_sha256": topology.sha256,
            "inventory": [asdict(member) for member in inventory], "artifacts": payloads,
            "application_executables": exports})
        manifest.update(schema=ARCHIVE_SCHEMA, deployment=deployment.projection(), topology_sha256=topology.sha256,
                        artifact_origins=origins)
    epoch = request["source_date_epoch"]
    if type(epoch) is not int or epoch < 0:
        raise ValueError("source_date_epoch must be a nonnegative integer")
    manifest["build_inputs"] = collect_build_inputs(request)
    manifest_bytes = (json.dumps(manifest, sort_keys=True, indent=2) + "\n").encode("utf-8")
    from contracts import MAX_JSON
    require(len(manifest_bytes) <= MAX_JSON, "build_inventory", "Release manifest exceeds its bounded JSON envelope.")
    budget.charge("manifest.json", len(manifest_bytes))
    (stage / "manifest.json").write_bytes(manifest_bytes)
    output.mkdir(parents=True, exist_ok=True)
    archive = output / "app.tar.gz"
    if archive.exists() or archive.is_symlink():
        raise ValueError("release output already exists")
    temporary = output / "app.tar.gz.partial"
    raw = temporary.open("xb")
    try:
        with raw, gzip.GzipFile(filename="", mode="xb", fileobj=_ArchiveOutput(raw, budget), mtime=0) as compressed, \
                tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as tar:
            for path in sorted(stage.rglob("*")):
                info = tar.gettarinfo(str(path), path.relative_to(stage).as_posix())
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mode = 0o755 if path.is_dir() or info.name == "artisan" or kinds.get(info.name) == "application" else 0o644
                info.mtime = epoch
                info.pax_headers = {}
                if info.isfile():
                    with path.open("rb") as content:
                        tar.addfile(info, content)
                else:
                    tar.addfile(info)
        os.replace(temporary, archive)
        (output / "manifest.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    finally:
        temporary.unlink(missing_ok=True)
    return archive


if __name__ == "__main__":
    request = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
    if sys.argv[1] == "public-env":
        public_env(request, sys.argv[3])
    elif sys.argv[1] == "package":
        package(request, sys.argv[3], sys.argv[4])
    elif sys.argv[1] == "prepare":
        prepared = prepare(request, sys.argv[3], sys.argv[4])
        Path(sys.argv[5]).write_text(json.dumps(prepared, sort_keys=True) + "\n", encoding="utf-8")
    else:
        raise SystemExit("unknown build metadata operation")
