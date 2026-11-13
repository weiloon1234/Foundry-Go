"""Pinned payload acquisition and immutable artifact trees.

M3 supplies the host/instance lock, release selection, ABI observation and protected
service-local mounts. One ExpansionBudget must span outer extraction and every
payload; publication and retention never select a release or start a workload.

Integration: parse manifest['deployment'] with ReleaseDeclaration; charge the
outer upload with retain_archive and every extracted header with charge. Pass
that budget to verify_release_tree(..., outer_charged=True) and publish. Persist
references/history before publication. bundle_export returns only a verified
read-only source: use the frozen ArtifactMount area/path for M3's protected view.
RetentionInputs includes namespace mounts and incomplete observations as well as
releases/operations/processes; retention_reasons grants no deletion authority.
"""
from __future__ import annotations

from dataclasses import dataclass
import gzip
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
from urllib.parse import urljoin, urlsplit

from contracts import (ArtifactPayload, DeployError, PayloadMember, ReleaseDeclaration,
                       check_path, contract_digest, contract_name, contract_path,
                       digest, require)
from topology import Artifact, ArtifactPlatform, ExecutableRole, parse_profile, parse_topology

CHUNK = 1024 * 1024
MAX_REDIRECTS = 5
DOWNLOAD_SECONDS = 120
MAX_METADATA_BYTES = 16 * 1024 * 1024
# Translation prefixes and a platform control VIP need explicit exclusions on
# supported Python versions whose global-address tables predate these entries.
SPECIAL_DOWNLOAD_NETWORKS = tuple(ipaddress.ip_network(network) for network in
                                  ("64:ff9b::/96", "64:ff9b:1::/48", "168.63.129.16/32"))


@dataclass
class ExpansionBudget:
    """Cumulative reservations, including duplicates and already-published trees.

    Charge outer members before writing them. Payload inspection charges inner
    members even if publication can reuse an existing tree. Never refund charges
    or allocate a new budget for each artifact. disk_bytes includes retained
    compressed input as well as output; block rounding bounds many tiny files.
    """
    max_bytes: int | None = None
    max_files: int | None = None
    max_disk_bytes: int | None = None
    max_metadata_bytes: int = MAX_METADATA_BYTES
    bytes: int = 0
    files: int = 0
    disk_bytes: int = 0
    metadata_bytes: int = 0

    def __post_init__(self):
        # Keep the existing release limits authoritative, including legacy input.
        from release import MAX_EXPANDED_BYTES, MAX_FILES
        if self.max_bytes is None:
            self.max_bytes = MAX_EXPANDED_BYTES
        if self.max_files is None:
            self.max_files = MAX_FILES
        if self.max_disk_bytes is None:
            self.max_disk_bytes = self.max_bytes

    def charge(self, path: str, size: int, *, directory=False):
        contract_path(path)
        require(type(size) is int and size >= 0, "artifact_size", "Invalid payload size.")
        self.files += 1
        self.bytes += size
        self.metadata_bytes += len(path.encode("utf-8")) + 512
        self.disk_bytes += 4096 if directory else ((size + 4095) // 4096) * 4096
        self._check()

    def grow(self, previous_size: int, size: int, *, expanded=True):
        """Charge a streamed compressed payload before writing each block."""
        if expanded:
            self.bytes += size
        self.disk_bytes += (((previous_size + size + 4095) // 4096) - ((previous_size + 4095) // 4096)) * 4096
        self._check()

    def retain_archive(self, size: int):
        """Account for the retained outer upload/archive in staged disk usage."""
        require(type(size) is int and size >= 0, "artifact_size", "Invalid retained archive size.")
        self.grow(0, size, expanded=False)

    def _check(self):
        require(self.files <= self.max_files and self.bytes <= self.max_bytes
                and self.disk_bytes <= self.max_disk_bytes and self.metadata_bytes <= self.max_metadata_bytes,
                "artifact_expansion", "Release exceeds its aggregate expansion budget.")


def _remaining(deadline):
    remaining = deadline - time.monotonic()
    require(remaining > 0, "artifact_timeout", "Artifact acquisition exceeded its deadline.")
    return remaining


def _public_address(value):
    try:
        address = ipaddress.ip_address(value)
    except ValueError:
        raise DeployError("artifact_origin", "Download origin has an invalid address.") from None
    require(address.is_global and not address.is_multicast and not address.is_reserved
            and not getattr(address, "ipv4_mapped", None) and not getattr(address, "sixtofour", None)
            and not getattr(address, "teredo", None) and not address.is_unspecified
            and not any(address.version == network.version and address in network for network in SPECIAL_DOWNLOAD_NETWORKS),
            "artifact_origin", "Download origin must resolve exclusively to public addresses.")
    return address


def _url(value):
    ArtifactPlatform.parse({"url": value, "sha256": "0" * 64})
    require("?" not in value and "#" not in value and "@" not in urlsplit(value).netloc,
            "artifact_origin", "Download URLs cannot contain credentials, queries or fragments.")
    parsed = urlsplit(value)
    hostname = parsed.hostname
    try:
        address = ipaddress.ip_address(hostname)
    except ValueError:
        # inet_aton accepts historical decimal, octal, hex and abbreviated IPv4.
        try:
            socket.inet_aton(hostname)
        except OSError:
            pass
        else:
            raise DeployError("artifact_origin", "Noncanonical IP literal in download URL.") from None
        require(hostname == hostname.lower() and not hostname.endswith("."),
                "artifact_origin", "Download hostname must be canonical.")
    else:
        _public_address(str(address))
        require(hostname == str(address), "artifact_origin", "Download IP literal must be canonical.")
    return parsed


def _resolve(hostname, timeout):
    # Bound libc DNS as well as socket I/O. A stuck resolver cannot retain a
    # background thread after the download returns. No proxy libraries are used.
    script = ("import json,socket,sys; "
              "rows=socket.getaddrinfo(sys.argv[1],443,type=socket.SOCK_STREAM); "
              "sys.exit(2) if len(rows)>64 else print(json.dumps([r[4][0] for r in rows]))")
    result = subprocess.run([sys.executable, "-c", script, hostname], stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, timeout=timeout, check=True)
    require(len(result.stdout) <= 16384, "artifact_origin", "Download DNS result is too large.")
    return json.loads(result.stdout)


class _PinnedHTTPS(http.client.HTTPSConnection):
    def __init__(self, hostname, address, timeout):
        super().__init__(hostname, port=443, timeout=timeout, context=ssl.create_default_context())
        self.address = address

    def connect(self):
        address = _public_address(self.address)
        connection = socket.socket(socket.AF_INET6 if address.version == 6 else socket.AF_INET, socket.SOCK_STREAM)
        try:
            connection.settimeout(self.timeout)
            connection.connect((str(address), 443))
            require(ipaddress.ip_address(connection.getpeername()[0]) == address,
                    "artifact_peer", "Download connection differs from the approved DNS result.")
            self.sock = self._context.wrap_socket(connection, server_hostname=self.host)
        except BaseException:
            connection.close()
            raise


@dataclass(frozen=True)
class DownloadOrigin:
    url: str
    address: str


def download(pin: ArtifactPlatform, destination: Path, *, max_bytes=None, seconds=DOWNLOAD_SECONDS,
             resolver=None, connection_factory=None, budget=None, budget_path=None) -> tuple[DownloadOrigin, ...]:
    """Fetch only verified HTTPS bytes; injectable DNS/transport keep tests offline.

    No environment/system proxy discovery occurs. Every attempt resolves afresh,
    rejects the whole answer if any address is forbidden, and connects numerically.
    """
    from host import OperationTimeout, wall_clock_budget
    require(0 < seconds <= DOWNLOAD_SECONDS, "artifact_timeout", "Invalid download deadline.")
    try:
        with wall_clock_budget(seconds):
            return _download(pin, destination, max_bytes=max_bytes, seconds=seconds, resolver=resolver,
                             connection_factory=connection_factory, budget=budget, budget_path=budget_path)
    except OperationTimeout:
        raise DeployError("artifact_timeout", "Artifact acquisition exceeded its deadline.") from None


def _download(pin, destination, *, max_bytes, seconds, resolver, connection_factory, budget, budget_path):
    from host import OperationTimeout
    from release import MAX_ARCHIVE_BYTES
    max_bytes = MAX_ARCHIVE_BYTES if max_bytes is None else max_bytes
    require(type(max_bytes) is int and 0 < max_bytes <= MAX_ARCHIVE_BYTES,
            "artifact_size", "Download size limit exceeds the supported archive limit.")
    resolver = resolver or _resolve
    connection_factory = connection_factory or _PinnedHTTPS
    destination = check_path(destination, "file")
    require(not destination.exists(), "artifact_conflict", "Artifact destination already exists.")
    contract_digest(pin.sha256)
    deadline = time.monotonic() + seconds
    url, seen, origins = pin.url, set(), []
    temporary = None
    try:
        for _ in range(MAX_REDIRECTS + 1):
            parsed = _url(url)
            require(url not in seen, "artifact_redirect", "Artifact redirect loop.")
            seen.add(url)
            redirected = False
            for attempt in range(2):
                connection = None
                try:
                    addresses = resolver(parsed.hostname, _remaining(deadline))
                    require(isinstance(addresses, (list, tuple)) and 1 <= len(addresses) <= 64,
                            "artifact_origin", "Download origin has no bounded address result.")
                    approved = sorted({str(_public_address(address)) for address in addresses})
                    address = approved[attempt % len(approved)]
                    connection = connection_factory(parsed.hostname, address, _remaining(deadline))
                    connection.connect()
                    peer_socket = connection.sock
                    require(str(_public_address(peer_socket.getpeername()[0])) == address,
                            "artifact_peer", "Download connected to an unapproved peer.")
                    peer_socket.settimeout(_remaining(deadline))
                    connection.request("GET", parsed.path or "/", headers={"Accept-Encoding": "identity"})
                    response = connection.getresponse()
                    origins.append(DownloadOrigin(url, address))
                    if response.status in (301, 302, 303, 307, 308):
                        location = response.getheader("Location")
                        require(isinstance(location, str) and 0 < len(location) <= 2048,
                                "artifact_redirect", "Invalid artifact redirect.")
                        url = urljoin(url, location)
                        _url(url)
                        redirected = True
                        break
                    require(response.status == 200 and response.getheader("Content-Encoding", "identity") == "identity",
                            "artifact_download", "Download did not return an unencoded payload.")
                    length = response.getheader("Content-Length")
                    require(length is None or length.isascii() and length.isdigit() and int(length) <= max_bytes,
                            "artifact_size", "Download exceeds its compressed size limit.")
                    descriptor, name = tempfile.mkstemp(prefix=".artifact-", dir=destination.parent)
                    temporary = Path(name)
                    total, checksum = 0, hashlib.sha256()
                    with os.fdopen(descriptor, "wb") as output:
                        if budget is not None:
                            budget.charge(budget_path, 0)
                        while True:
                            peer_socket.settimeout(_remaining(deadline))
                            block = response.read1(min(CHUNK, max_bytes - total + 1))
                            if not block:
                                break
                            if budget is not None:
                                budget.grow(total, len(block))
                            total += len(block)
                            require(total <= max_bytes, "artifact_size", "Download exceeds its compressed size limit.")
                            checksum.update(block)
                            require(shutil.disk_usage(destination.parent).free >= len(block) + 4096,
                                    "artifact_disk", "Insufficient staging disk space for artifact.")
                            output.write(block)
                        require((length is None or total == int(length)) and checksum.hexdigest() == pin.sha256,
                                "artifact_checksum", "Artifact differs from its declared SHA-256 pin.")
                        output.flush()
                        os.fsync(output.fileno())
                    temporary.chmod(0o444)
                    os.link(temporary, destination, follow_symlinks=False)
                    _sync(destination.parent)
                    return tuple(origins)
                except OperationTimeout:
                    raise
                except (OSError, http.client.HTTPException, subprocess.SubprocessError):
                    if attempt == 1:
                        raise DeployError("artifact_download", "Artifact acquisition failed within its bounded attempts.") from None
                finally:
                    if connection is not None:
                        connection.close()
                    if temporary is not None:
                        temporary.unlink(missing_ok=True)
                        temporary = None
            require(redirected, "artifact_download", "Artifact acquisition did not complete.")
        raise DeployError("artifact_redirect", "Artifact has too many redirects.")
    except (ValueError, UnicodeError):
        raise DeployError("artifact_origin", "Invalid download origin or DNS response.") from None


def _sync(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _safe_path(name):
    contract_path(name)
    parts = Path(name).parts
    require(not any(p.startswith(".env") or p in (".git", "node_modules") for p in parts)
            and Path(name).suffix.lower() not in (".pem", ".key", ".p12", ".pfx"),
            "artifact_private", "Payload contains a private or key-material path.")
    return name


def validate_binary(header: bytes, platform: str, *, module=False):
    require(platform in ("linux-amd64", "linux-arm64"), "artifact_architecture", "Unsupported artifact platform.")
    require(len(header) >= 64 and header[:6] == b"\x7fELF\x02\x01"
            and header[6] == 1 and header[7] in (0, 3)
            and int.from_bytes(header[18:20], "little") == {"linux-amd64": 62, "linux-arm64": 183}[platform]
            and int.from_bytes(header[16:18], "little") in ((3,) if module else (2, 3)),
            "artifact_architecture", "Executable/module must be a compatible Linux ELF binary.")


def _kind(artifact, name, mode, header, platform):
    if name in dict(artifact.executables).values():
        require(name not in artifact.modules, "artifact_inventory", "A module cannot also be an executable export.")
        validate_binary(header, platform)
        return "application"
    if name in artifact.modules:
        validate_binary(header, platform, module=True)
        return "module"
    require(not mode & 0o111 and not header.startswith((b"\x7fELF", b"#!", b"MZ"))
            and Path(name).suffix.lower() not in (".so", ".dll", ".dylib") and ".so." not in Path(name).name,
            "artifact_inventory", "Payload contains undeclared executable or module code.")
    return "data"


class _BoundedTar:
    def __init__(self, source, limit):
        self.source, self.remaining = source, limit

    def read(self, size):
        block = self.source.read(min(size, self.remaining + 1, CHUNK))
        self.remaining -= len(block)
        require(self.remaining >= 0, "artifact_expansion", "Tar stream exceeds its aggregate metadata/data budget.")
        return block


class _ArtifactTarInfo(tarfile.TarInfo):
    def _proc_pax(self, tarfile_object):
        require(self.size <= 65536, "artifact_expansion", "Artifact extended header is too large.")
        self.expansion_budget.metadata_bytes += self.size
        self.expansion_budget._check()
        return super()._proc_pax(tarfile_object)

    def _proc_gnulong(self, tarfile_object):
        require(self.size <= 4096, "artifact_expansion", "Artifact extended path is too large.")
        self.expansion_budget.metadata_bytes += self.size
        self.expansion_budget._check()
        return super()._proc_gnulong(tarfile_object)

    def _proc_sparse(self, tarfile_object):
        raise DeployError("artifact_archive", "Sparse artifact members are forbidden.")

    def _proc_gnusparse_00(self, *args):
        raise DeployError("artifact_archive", "Sparse artifact members are forbidden.")

    _proc_gnusparse_01 = _proc_gnusparse_00
    _proc_gnusparse_10 = _proc_gnusparse_00


def inspect_payload(artifact: Artifact, platform: str, payload: Path, budget: ExpansionBudget,
                    *, destination: Path | None = None) -> tuple[PayloadMember, ...]:
    """Verify the compressed pin before parsing, optionally extract into a new tree.

    Output owns no symlinks, writable code or implicit exports. Data archives are
    opaque files; executable/module exports must be ELF and are never recursed.
    """
    from release import MAX_ARCHIVE_BYTES
    pin = dict(artifact.platforms).get(platform)
    require(pin is not None, "artifact_architecture", "Artifact has no target architecture pin.")
    payload = check_path(payload, "file")
    require(payload.stat().st_size <= MAX_ARCHIVE_BYTES and digest(payload) == pin.sha256,
            "artifact_checksum", "Artifact payload differs from its pin.")
    if destination is not None:
        destination = check_path(destination, "directory")
        require(not destination.exists(), "artifact_conflict", "Artifact staging tree already exists.")
        destination.mkdir(mode=0o700)
    members, seen, directories, charged_directories = [], set(), set(), set()

    class BoundedTarInfo(_ArtifactTarInfo):
        expansion_budget = budget

    def parents(name):
        for parent in reversed(Path(name).parents[:-1]):
            directory = parent.as_posix()
            require(directory not in seen - directories, "artifact_archive", "Artifact file overlaps a parent directory.")
            if directory not in charged_directories:
                budget.charge(directory, 0, directory=True)
                charged_directories.add(directory)

    def consume(name, size, mode, source):
        _safe_path(name)
        require(name not in seen and name not in charged_directories and not mode & 0o6000,
                "artifact_archive", "Duplicate, overlapping or privileged artifact member.")
        require(not any(parent.as_posix() in seen - directories for parent in Path(name).parents if str(parent) != "."),
                "artifact_archive", "Artifact file overlaps a parent directory.")
        seen.add(name)
        parents(name)
        budget.charge(name, size)
        checksum, count, header = hashlib.sha256(), 0, b""
        target = destination / name if destination is not None else None
        output = None
        try:
            if target is not None:
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                output = target.open("xb")
            while count < size:
                block = source.read(min(CHUNK, size - count))
                require(bool(block), "artifact_archive", "Truncated artifact member.")
                if len(header) < 64:
                    header += block[:64 - len(header)]
                checksum.update(block)
                count += len(block)
                if output is not None:
                    require(shutil.disk_usage(target.parent).free >= len(block) + 4096,
                            "artifact_disk", "Insufficient staging disk space for artifact.")
                    output.write(block)
            kind = _kind(artifact, name, mode, header, platform)
            members.append(PayloadMember(name, checksum.hexdigest(), size, kind))
            if output is not None:
                output.flush()
                os.fchmod(output.fileno(), 0o555 if kind == "application" else 0o444)
                os.fsync(output.fileno())
        finally:
            if output is not None:
                output.close()

    try:
        if artifact.format == "file":
            with payload.open("rb") as source:
                consume(artifact.executables[0][1], payload.stat().st_size, 0o755, source)
        else:
            with gzip.open(payload, "rb") as expanded:
                bounded = _BoundedTar(expanded, max(0, budget.max_bytes - budget.bytes)
                                      + budget.max_metadata_bytes + budget.max_files * 1024)
                with tarfile.open(fileobj=bounded, mode="r|", tarinfo=BoundedTarInfo) as archive:
                    for item in archive:
                        name = _safe_path(item.name)
                        require((item.isfile() or item.isdir()) and not item.issparse()
                                and not item.mode & 0o6000 and not item.pax_headers.keys() - {"path", "mtime"},
                                "artifact_archive", "Unsupported artifact member or metadata.")
                        if item.isdir():
                            require(name not in seen and item.size == 0,
                                    "artifact_archive", "Duplicate or invalid artifact directory.")
                            seen.add(name)
                            directories.add(name)
                            parents(name)
                            if name not in charged_directories:
                                budget.charge(name, 0, directory=True)
                                charged_directories.add(name)
                            if destination is not None:
                                (destination / name).mkdir(parents=True, exist_ok=True, mode=0o700)
                        else:
                            with archive.extractfile(item) as source:
                                consume(name, item.size, item.mode, source)
        names = {m.path for m in members}
        require(1 <= len(members) <= 10000 and set(dict(artifact.executables).values()) <= names
                and set(artifact.modules) <= names
                and all(any(n.startswith(p + "/") for n in names) for p in directories)
                and all(any(n.startswith(p + "/") for n in names) for _, p in artifact.bundles),
                "artifact_inventory", "Artifact lacks a declared executable, module or bundle export.")
        if destination is not None:
            for directory in sorted((p for p in destination.rglob("*") if p.is_dir()), reverse=True):
                directory.chmod(0o555)
                _sync(directory)
            destination.chmod(0o555)
            _sync(destination)
        return tuple(sorted(members, key=lambda member: member.path))
    except (OSError, EOFError, tarfile.TarError):
        raise DeployError("artifact_archive", "Cannot inspect artifact payload safely.") from None
    finally:
        if sys.exc_info()[0] is not None and destination is not None:
            _remove_staging(destination)


def _remove_staging(path):
    if path.exists():
        for directory in [path, *(p for p in path.rglob("*") if p.is_dir())]:
            directory.chmod(0o700)
        shutil.rmtree(path)


def declared_release_files(topology, application_executables, payload_paths):
    """The shared schema-2 allowlist; never include an arbitrary deploy directory."""
    declared = {"artisan": "application", "config/deploy.toml": "config"}
    for role in topology.roles:
        if isinstance(role, ExecutableRole):
            declared.update({config.source: "config" for _, config in role.configs})
    for path in application_executables:
        require(path not in declared, "artifact_inventory", "Application export overlaps another declaration.")
        declared[path] = "application"
    for path in payload_paths:
        require(path not in declared, "artifact_inventory", "Artifact payload overlaps another declaration.")
        declared[path] = "artifact"
    return declared


def release_member_kind(name, declared):
    _safe_path(name)
    kind = declared.get(name, "asset" if name.startswith("public/") else
                        "source-map" if name.startswith("private-source-maps/") else None)
    require(kind is not None, "artifact_inventory", "Release contains an undeclared member.")
    return kind


def verify_release_tree(declaration: ReleaseDeclaration, root: Path, budget: ExpansionBudget,
                        *, outer_charged=False):
    """M3 calls after strict outer extraction, before publication/start.

    If its extractor already charged every outer header, pass outer_charged=True.
    manifest.json is the only file outside the explicit declaration inventory.
    The outer extractor must also call budget.retain_archive(upload.stat().st_size).
    """
    root = check_path(root, "directory")
    expected = {m.path: m for m in declaration.inventory}
    topology = parse_topology(declaration.topology_json)
    profile = parse_profile(declaration.profile_json, topology)
    declared = declared_release_files(topology, (path for _, path in declaration.application_executables),
                                      (payload.path for payload in declaration.artifacts))
    require(declared.keys() <= expected.keys(), "artifact_inventory", "Release lacks a required declared file.")
    for name, member in expected.items():
        require(member.kind == release_member_kind(name, declared),
                "artifact_inventory", "Release inventory member has an undeclared kind.")
    actual = set()
    for path in sorted(root.rglob("*")):
        check_path(path)
        name = path.relative_to(root).as_posix()
        _safe_path(name)
        require(path.is_file() or path.is_dir(), "artifact_inventory", "Nonregular release member.")
        require(not path.stat().st_mode & 0o6000, "artifact_inventory", "Privileged release member.")
        if path.is_dir():
            require(name in ("public", "private-source-maps") or name.startswith(("public/", "private-source-maps/"))
                    or any(n.startswith(name + "/") for n in expected),
                    "artifact_inventory", "Release contains an undeclared directory.")
        if not outer_charged:
            budget.charge(name, path.stat().st_size if path.is_file() else 0, directory=path.is_dir())
        if path.is_file() and name != "manifest.json":
            actual.add(name)
            member = expected.get(name)
            require(member is not None and path.stat().st_nlink == 1
                    and path.stat().st_size == member.size and digest(path) == member.sha256,
                    "artifact_inventory", "Release file differs from its explicit inventory.")
            if member.kind == "application":
                with path.open("rb") as binary:
                    validate_binary(binary.read(64), "linux-" + profile.architecture)
    require(actual == set(expected), "artifact_inventory", "Release inventory differs from staged files.")
    for payload in declaration.artifacts:
        members = inspect_payload(dict(topology.artifacts)[payload.artifact], payload.platform, root / payload.path, budget)
        require(members == tuple(sorted(payload.members, key=lambda m: m.path)),
                "artifact_inventory", "Extracted artifact differs from its release inventory.")


@dataclass(frozen=True, order=True)
class ArtifactReference:
    artifact: str
    version: str
    platform: str
    sha256: str

    def relative_path(self):
        contract_name(self.artifact)
        contract_path(self.version)
        require("/" not in self.version and self.platform in ("linux-amd64", "linux-arm64"),
                "artifact_reference", "Invalid immutable artifact reference.")
        contract_digest(self.sha256)
        return f"{self.artifact}/{self.version}/{self.platform}/{self.sha256}"


def references(declaration: ReleaseDeclaration) -> tuple[ArtifactReference, ...]:
    artifacts = dict(parse_topology(declaration.topology_json).artifacts)
    return tuple(ArtifactReference(p.artifact, artifacts[p.artifact].version, p.platform, p.sha256)
                 for p in declaration.artifacts)


def validate_history(candidate: tuple[ArtifactReference, ...], history: tuple[ArtifactReference, ...]):
    """Caller retains this history even after trees/releases have been pruned."""
    pins = {}
    for reference in (*history, *candidate):
        reference.relative_path()
        key = (reference.artifact, reference.version, reference.platform)
        require(key not in pins or pins[key] == reference.sha256,
                "artifact_version", "Artifact version has already been recorded with different bytes.")
        pins[key] = reference.sha256


def validate_abi(artifact: Artifact, observed_service_abi: str | None):
    """M3 supplies the ABI established for the selected service/package version.

    A free-form version guess is not evidence. Unknown compatibility refuses;
    actual module initialization remains part of the service readiness contract.
    """
    if artifact.bundles:
        require(artifact.provenance is not None and observed_service_abi is not None
                and artifact.provenance.service_abi == observed_service_abi,
                "artifact_abi", "Bundle compatibility with the selected service ABI is unproven.")


def verify_tree(path: Path, members: tuple[PayloadMember, ...], *, uid=0):
    path = check_path(path, "directory")
    expected, actual = {m.path: m for m in members}, set()
    for child in [path, *sorted(path.rglob("*"))]:
        check_path(child)
        info = child.stat()
        require(info.st_uid == uid and not info.st_mode & 0o222 and (child.is_file() or child.is_dir()),
                "artifact_ownership", "Immutable artifact tree has unsafe ownership or modes.")
        if child.is_dir():
            require(stat.S_IMODE(info.st_mode) == 0o555, "artifact_ownership", "Immutable artifact directory has unsafe modes.")
        if child.is_dir() and child != path:
            require(any(name.startswith(child.relative_to(path).as_posix() + "/") for name in expected),
                    "artifact_inventory", "Published artifact has an uninventoried directory.")
        if child.is_file():
            name = child.relative_to(path).as_posix()
            member = expected.get(name)
            require(member is not None and info.st_nlink == 1 and info.st_size == member.size
                    and stat.S_IMODE(info.st_mode) == (0o555 if member.kind == "application" else 0o444)
                    and digest(child) == member.sha256,
                    "artifact_inventory", "Published artifact tree differs from its inventory.")
            actual.add(name)
    require(actual == set(expected), "artifact_inventory", "Published artifact tree is incomplete.")


def publish(host, instance: str, artifact: Artifact, payload: ArtifactPayload, source: Path,
            budget: ExpansionBudget, *, history: tuple[ArtifactReference, ...], observed_service_abi=None) -> Path:
    """Publish under M3's host/instance locks. Never repair a selected tree in place.

    The caller journals the reference before invoking this function and retains
    it through unresolved outcomes. Source is the uploaded payload, never a URL.
    """
    reference = ArtifactReference(payload.artifact, artifact.version, payload.platform, payload.sha256)
    contract_name(instance)
    validate_history((reference,), history)
    validate_abi(artifact, observed_service_abi)
    require(dict(artifact.platforms)[payload.platform].sha256 == payload.sha256,
            "artifact_checksum", "Artifact reference differs from its pin.")
    base = host.paths.opt(instance) / "artifacts"
    host.check_control_directory(base.parent)
    target = base / reference.relative_path()
    for parent in reversed([target.parent, *target.parent.parents]):
        if parent == base or base in parent.parents:
            host.check_control_directory(parent)
            if not parent.exists():
                parent.mkdir(mode=0o755)
                parent.chmod(0o755)
                _sync(parent.parent)
            require(stat.S_IMODE(parent.stat().st_mode) == 0o755,
                    "artifact_ownership", "Artifact publication ancestry must be service-readable and root-writable only.")
    check_path(target, "directory")
    staging = Path(tempfile.mkdtemp(prefix=".artifact-", dir=target.parent))
    staging.rmdir()
    try:
        members = inspect_payload(artifact, payload.platform, source, budget, destination=staging)
        require(members == tuple(sorted(payload.members, key=lambda m: m.path)),
                "artifact_inventory", "Artifact members differ from the release inventory.")
        uid = 0 if host.paths.root == Path("/") else os.geteuid()
        verify_tree(staging, members, uid=uid)
        if target.exists():
            verify_tree(target, members, uid=uid)
        else:
            os.rename(staging, target)
            _sync(target.parent)
        return target
    finally:
        _remove_staging(staging)


@dataclass(frozen=True)
class BundleExport:
    """Source mapping only: M3 creates the protected service-local mount/view."""
    reference: ArtifactReference
    bundle: str
    source: Path
    read_only: bool = True


def bundle_export(reference: ArtifactReference, artifact: Artifact, tree: Path, bundle: str,
                  members: tuple[PayloadMember, ...], *, observed_service_abi, uid=0) -> BundleExport:
    validate_abi(artifact, observed_service_abi)
    require(tree.as_posix().endswith("/artifacts/" + reference.relative_path()),
            "artifact_reference", "Bundle source is not its canonical immutable artifact tree.")
    require(reference.version == artifact.version and reference.sha256 == dict(artifact.platforms)[reference.platform].sha256
            and bundle in dict(artifact.bundles), "artifact_reference", "Unknown bundle export or version.")
    verify_tree(tree, members, uid=uid)
    source = check_path(tree / dict(artifact.bundles)[bundle], "directory")
    require(source.is_dir(), "artifact_inventory", "Bundle export directory is missing.")
    return BundleExport(reference, bundle, source)


@dataclass(frozen=True)
class RetentionInputs:
    retained_releases: tuple[ArtifactReference, ...] = ()
    selections: tuple[ArtifactReference, ...] = ()
    operations: tuple[ArtifactReference, ...] = ()
    processes: tuple[ArtifactReference, ...] = ()
    mounts: tuple[ArtifactReference, ...] = ()
    backups: tuple[ArtifactReference, ...] = ()
    observations_complete: bool = False


def retention_reasons(reference: ArtifactReference, inputs: RetentionInputs) -> tuple[str, ...]:
    """Advisory inputs for M3 cleanup; absence is usable only under its locks.

    No deletion occurs here. Retained backup references also retain the complete
    release archive, independently of this artifact tree.
    """
    reference.relative_path()
    reasons = tuple(name for name in ("retained_releases", "selections", "operations", "processes", "mounts", "backups")
                    if reference in getattr(inputs, name))
    return reasons + (() if inputs.observations_complete else ("unknown_observations",))
