"""Supply-chain fixtures: no DNS, sockets, remote downloads or host services."""
from dataclasses import replace
import gzip
import hashlib
import io
import os
from pathlib import Path
import socket
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import artifacts as a
from contracts import ArtifactPayload, DeployError
from host import Host, OperationTimeout, Paths
from topology import Artifact, ArtifactPlatform

PUBLIC = "93.184.216.34"
URL = "https://downloads.example.org/payload"


def checksum(content):
    return hashlib.sha256(content).hexdigest()


def elf(platform="linux-amd64", *, module=False):
    header = bytearray(64)
    header[:7] = b"\x7fELF\x02\x01\x01"
    header[16:18] = (3 if module else 2).to_bytes(2, "little")
    header[18:20] = (62 if platform == "linux-amd64" else 183).to_bytes(2, "little")
    return bytes(header) + b"neutral-fixture"


def tar_bytes(members):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w:gz", format=tarfile.PAX_FORMAT) as archive:
        for name, content, mode, kind in members:
            info = tarfile.TarInfo(name)
            info.mode, info.type = mode, kind
            info.size = len(content) if kind == tarfile.REGTYPE else 0
            if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                info.linkname = "../outside"
            archive.addfile(info, io.BytesIO(content) if info.isfile() else None)
    return output.getvalue()


def bundle_fixture(platform="linux-amd64"):
    content = tar_bytes([("plugin/extension.so", elf(platform, module=True), 0o644, tarfile.REGTYPE),
                         ("plugin/data.dat", b"companion data", 0o644, tarfile.REGTYPE)])
    raw = {"version": "1.2.3", "format": "tar.gz", "platforms": {platform: {"url": URL, "sha256": checksum(content)}},
           "bundles": {"plugin": "plugin"}, "modules": ["plugin/extension.so"],
           "provenance": {"source_revision": "revision-1", "build_inputs": "toolchain-1", "license": "MIT", "service_abi": "service-1"}}
    return Artifact.parse(raw), content


class Response:
    def __init__(self, body=b"payload", status=200, headers=None):
        self.body, self.status, self.headers = io.BytesIO(body), status, headers or {}

    def getheader(self, name, default=None):
        return self.headers.get(name, default)

    def read1(self, size):
        return self.body.read(size)


class Connection:
    def __init__(self, address, response):
        self.sock = Mock()
        self.sock.getpeername.return_value = (address, 443)
        self.response, self.requests, self.closed = response, [], False

    def connect(self):
        pass

    def request(self, *args, **kwargs):
        self.requests.append((args, kwargs))

    def getresponse(self):
        if isinstance(self.response, BaseException):
            raise self.response
        return self.response

    def close(self):
        self.closed = True


class DownloadTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.destination = self.root / "payload"
        self.pin = ArtifactPlatform(URL, checksum(b"payload"))
        self.resolver = Mock(return_value=[PUBLIC])
        self.connections = []
        self.responses = [Response()]
        patcher = patch.object(socket, "getaddrinfo", side_effect=AssertionError("real DNS is forbidden"))
        patcher.start()
        self.addCleanup(patcher.stop)

    def factory(self, hostname, address, timeout):
        connection = Connection(address, self.responses.pop(0))
        self.connections.append(connection)
        return connection

    def download(self, **kwargs):
        return a.download(self.pin, self.destination, resolver=self.resolver, connection_factory=self.factory, **kwargs)

    def test_verified_payload_uses_pinned_peer_ignores_all_proxy_environment(self):
        environment = {key: "http://127.0.0.1:1" for key in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy")}
        with patch.dict(os.environ, environment):
            origins = self.download()
        self.assertEqual(self.destination.read_bytes(), b"payload")
        self.assertEqual(self.destination.stat().st_mode & 0o777, 0o444)
        self.assertEqual(origins, (a.DownloadOrigin(URL, PUBLIC),))
        self.assertEqual(self.connections[0].requests[0][0], ("GET", "/payload"))
        self.assertTrue(self.connections[0].closed)

    def test_initial_urls_reject_forbidden_literal_forms_without_dns_or_requests(self):
        for origin in ("http://example.org/p", "https://user@example.org/p", "https://example.org/p?",
                       "https://example.org/p#", "https://example.org/p?token=secret", "https://127.0.0.1/p",
                       "https://169.254.169.254/p", "https://[::1]/p", "https://[::ffff:127.0.0.1]/p",
                       "https://127.1/p", "https://2130706433/p", "https://0x7f000001/p", "https://0177.0.0.1/p",
                       "https://[64:ff9b::7f00:1]/p"):
            self.pin = ArtifactPlatform(origin, checksum(b"payload"))
            with self.subTest(origin=origin), self.assertRaises(DeployError):
                self.download()
        self.resolver.assert_not_called()
        self.assertEqual(self.connections, [])

    def test_any_private_dns_result_rejects_whole_answer_before_connect(self):
        for address in ("127.0.0.1", "10.0.0.1", "169.254.169.254", "168.63.129.16", "fd00::1",
                        "::ffff:8.8.8.8", "64:ff9b:1::a00:1", "224.0.0.1"):
            self.resolver.return_value = [PUBLIC, address]
            with self.subTest(address=address), self.assertRaises(DeployError):
                self.download()
        self.assertEqual(self.connections, [])

    def test_peer_rebinding_refuses_before_request(self):
        connection = Connection("127.0.0.1", Response())
        with self.assertRaises(DeployError):
            a.download(self.pin, self.destination, resolver=self.resolver, connection_factory=lambda *args: connection)
        self.assertEqual(connection.requests, [])
        self.assertTrue(connection.closed)

    def test_retry_reresolves_and_revalidates_dns(self):
        self.responses = [OSError("connection lost")]
        self.resolver.side_effect = [[PUBLIC], ["127.0.0.1"]]
        with self.assertRaises(DeployError):
            self.download()
        self.assertEqual(self.resolver.call_count, 2)
        self.assertEqual(len(self.connections), 1)
        self.assertFalse(self.destination.exists())

    def test_each_redirect_revalidates_origin_and_dns(self):
        for target in ("http://downloads.example.org/other", "https://127.0.0.1/other",
                       "https://downloads.example.org/other?secret=x", "https://user@downloads.example.org/other"):
            self.responses = [Response(status=302, headers={"Location": target})]
            with self.subTest(target=target), self.assertRaises(DeployError):
                self.download()
        self.responses = [Response(status=307, headers={"Location": "https://other.example.org/p"})]
        self.resolver.side_effect = [[PUBLIC], ["10.0.0.1"]]
        with self.assertRaises(DeployError):
            self.download()
        self.assertEqual(self.resolver.call_args[0][0], "other.example.org")

    def test_redirect_loops_and_limits_leave_no_output(self):
        self.responses = [Response(status=302, headers={"Location": URL})]
        with self.assertRaises(DeployError):
            self.download()
        self.responses = [Response(status=302, headers={"Location": f"/next-{i}"}) for i in range(a.MAX_REDIRECTS + 1)]
        with self.assertRaises(DeployError):
            self.download()
        self.assertEqual(list(self.root.iterdir()), [])

    def test_size_checksum_content_encoding_and_truncation_fail_before_use(self):
        for response, limit in ((Response(body=b"wrong"), 100), (Response(), 3),
                                (Response(headers={"Content-Length": "99"}), 100),
                                (Response(headers={"Content-Length": "101"}), 100),
                                (Response(headers={"Content-Encoding": "gzip"}), 100)):
            self.responses = [response]
            with self.subTest(headers=response.headers, limit=limit), self.assertRaises(DeployError):
                self.download(max_bytes=limit)
            self.assertEqual(list(self.root.iterdir()), [])

    def test_absolute_deadline_is_not_retried(self):
        self.responses = [OperationTimeout()]
        with self.assertRaises(DeployError) as error:
            self.download()
        self.assertEqual(error.exception.code, "artifact_timeout")
        self.assertEqual(self.resolver.call_count, 1)

    def test_streaming_download_charges_shared_disk_budget_before_write(self):
        budget = a.ExpansionBudget(max_disk_bytes=4095)
        with self.assertRaises(DeployError):
            self.download(budget=budget, budget_path="deploy/artifacts/payload")
        self.assertEqual(list(self.root.iterdir()), [])

    def test_numeric_connect_preserves_tls_hostname_without_second_resolution(self):
        connection = Mock()
        connection.getpeername.return_value = (PUBLIC, 443)
        context = Mock()
        with patch.object(a.socket, "socket", return_value=connection), \
                patch.object(a.ssl, "create_default_context", return_value=context):
            client = a._PinnedHTTPS("downloads.example.org", PUBLIC, 30)
            client.connect()
        connection.connect.assert_called_once_with((PUBLIC, 443))
        context.wrap_socket.assert_called_once_with(connection, server_hostname="downloads.example.org")


class PayloadTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.payload = self.root / "payload"
        self.destination = self.root / "tree"
        self.addCleanup(lambda: a._remove_staging(self.destination))

    def inspect(self, artifact, content, budget=None, *, extract=False):
        self.payload.write_bytes(content)
        return a.inspect_payload(artifact, "linux-amd64", self.payload, budget or a.ExpansionBudget(),
                                 destination=self.destination if extract else None)

    def artifact(self, content, **fields):
        return Artifact.parse({"version": "v1", "format": "tar.gz", "executables": {"server": "bin/server"},
                               "platforms": {"linux-amd64": {"url": URL, "sha256": checksum(content)}}, **fields})

    def test_bundle_inventory_preserves_layout_hashes_modules_data_and_read_only_modes(self):
        artifact, content = bundle_fixture()
        members = self.inspect(artifact, content, extract=True)
        self.assertEqual([(m.path, m.kind) for m in members], [("plugin/data.dat", "data"), ("plugin/extension.so", "module")])
        for member in members:
            self.assertEqual(a.digest(self.destination / member.path), member.sha256)
            self.assertEqual((self.destination / member.path).stat().st_mode & 0o777, 0o444)
        self.assertEqual(self.destination.stat().st_mode & 0o777, 0o555)
        a.verify_tree(self.destination, members, uid=os.geteuid())

    def test_digest_is_verified_before_tar_parser_or_output_creation(self):
        artifact, content = bundle_fixture()
        with patch.object(a.tarfile, "open", side_effect=AssertionError("must not parse")):
            with self.assertRaises(DeployError) as error:
                self.inspect(artifact, content + b"tampered", extract=True)
        self.assertEqual(error.exception.code, "artifact_checksum")
        self.assertFalse(self.destination.exists())

    def test_member_attacks_and_wrong_architecture_refuse_and_remove_staging(self):
        good = ("bin/server", elf(), 0o755, tarfile.REGTYPE)
        attacks = [("../escape", b"x", 0o644, tarfile.REGTYPE), ("/absolute", b"x", 0o644, tarfile.REGTYPE),
                   ("bin/server", elf(), 0o755, tarfile.REGTYPE), ("link", b"", 0o644, tarfile.SYMTYPE),
                   ("hard", b"", 0o644, tarfile.LNKTYPE), ("fifo", b"", 0o644, tarfile.FIFOTYPE),
                   ("device", b"", 0o644, tarfile.CHRTYPE), ("setuid", b"x", 0o4644, tarfile.REGTYPE),
                   ("hidden-code", elf(), 0o644, tarfile.REGTYPE), ("script", b"#!/bin/sh\n", 0o644, tarfile.REGTYPE),
                   ("extra.so", b"code", 0o644, tarfile.REGTYPE), (".env.local", b"secret", 0o644, tarfile.REGTYPE)]
        for member in attacks:
            content = tar_bytes([good, member])
            with self.subTest(member=member[0]), self.assertRaises(DeployError):
                self.inspect(self.artifact(content), content, extract=True)
            self.assertFalse(self.destination.exists())
        for body in (elf("linux-arm64"), gzip.compress(b"nested archive"), b"#!/bin/sh\nexit 0"):
            content = tar_bytes([("bin/server", body, 0o755, tarfile.REGTYPE)])
            with self.assertRaises(DeployError):
                self.inspect(self.artifact(content), content)

    def test_data_archives_are_opaque_and_never_recursively_expanded(self):
        content = tar_bytes([("plugin/data.tar.gz", gzip.compress(b"ordinary data"), 0o644, tarfile.REGTYPE)])
        artifact, _ = bundle_fixture()
        artifact = replace(artifact, modules=(), platforms=(("linux-amd64", ArtifactPlatform(URL, checksum(content))),))
        members = self.inspect(artifact, content)
        self.assertEqual([(m.path, m.kind) for m in members], [("plugin/data.tar.gz", "data")])

    def test_missing_module_and_empty_bundle_refuse(self):
        artifact, _ = bundle_fixture()
        for content in (tar_bytes([("plugin/data.dat", b"data", 0o644, tarfile.REGTYPE)]), tar_bytes([])):
            changed = replace(artifact, platforms=(("linux-amd64", ArtifactPlatform(URL, checksum(content))),))
            with self.assertRaises(DeployError):
                self.inspect(changed, content)

    def test_module_architecture_and_loadable_type_are_checked(self):
        artifact, _ = bundle_fixture()
        for binary in (elf("linux-arm64", module=True), elf(module=False)):
            content = tar_bytes([("plugin/extension.so", binary, 0o644, tarfile.REGTYPE)])
            changed = replace(artifact, platforms=(("linux-amd64", ArtifactPlatform(URL, checksum(content))),))
            with self.assertRaises(DeployError) as error:
                self.inspect(changed, content)
            self.assertEqual(error.exception.code, "artifact_architecture")

    def test_sparse_formats_are_refused_before_sparse_map_processing(self):
        header = tarfile.TarInfo("bin/server")
        header.type = tarfile.GNUTYPE_SPARSE
        content = gzip.compress(header.tobuf() + b"\0" * 1024)
        with self.assertRaises(DeployError):
            self.inspect(self.artifact(content), content)
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz", format=tarfile.PAX_FORMAT) as archive:
            header = tarfile.TarInfo("bin/server")
            header.size = len(elf())
            header.pax_headers = {"GNU.sparse.map": "0,64", "GNU.sparse.size": "64"}
            archive.addfile(header, io.BytesIO(elf()))
        content = output.getvalue()
        with self.assertRaises(DeployError):
            self.inspect(self.artifact(content), content)

    def test_missing_companion_data_or_linked_bundle_tree_refuses(self):
        artifact, content = bundle_fixture()
        members = self.inspect(artifact, content, extract=True)
        directory = self.destination / "plugin"
        directory.chmod(0o755)
        (directory / "data.dat").unlink()
        directory.chmod(0o555)
        with self.assertRaises(DeployError):
            a.verify_tree(self.destination, members, uid=os.geteuid())
        link = self.root / "linked-tree"
        link.symlink_to(self.destination)
        with self.assertRaises(DeployError):
            a.verify_tree(link, members, uid=os.geteuid())

    def test_shared_budget_counts_outer_payload_and_repeated_inner_members(self):
        artifact, content = bundle_fixture()
        inner = a.ExpansionBudget()
        members = self.inspect(artifact, content, inner)
        outer = a.ExpansionBudget()
        outer.charge("deploy/artifacts/payload", len(content))
        expected = {name: getattr(outer, name) + 2 * getattr(inner, name)
                    for name in ("bytes", "files", "disk_bytes", "metadata_bytes")}
        self.assertEqual(expected["bytes"], len(content) + 2 * sum(member.size for member in members))
        self.assertEqual(expected["files"], 1 + 2 * (len(members) + 1))  # Include each plugin directory.
        for name, limit in expected.items():
            # Isolate each bound: a tiny byte limit must not also cap block-rounded disk usage.
            limits = {"max_disk_bytes": outer.max_disk_bytes, "max_" + name: limit}
            with self.subTest(limit=name):
                limited = a.ExpansionBudget(**limits)
                limited.charge("deploy/artifacts/payload", len(content))
                self.inspect(artifact, content, limited)
                self.inspect(artifact, content, limited)
                self.assertEqual({key: getattr(limited, key) for key in expected}, expected)
                with self.assertRaises(DeployError) as error:
                    self.inspect(artifact, content, limited)
                self.assertEqual(error.exception.code, "artifact_expansion")

    def test_oversized_pax_header_refuses_before_allocation(self):
        header = tarfile.TarInfo("pax")
        header.type, header.size = tarfile.XHDTYPE, 65537
        content = gzip.compress(header.tobuf() + b"\0" * 1024)
        with self.assertRaises(DeployError) as error:
            self.inspect(self.artifact(content), content)
        self.assertEqual(error.exception.code, "artifact_expansion")

    def test_single_file_export_checks_architecture_and_normalizes_executable(self):
        content = elf()
        artifact = self.artifact(content, format="file")
        members = self.inspect(artifact, content, extract=True)
        self.assertEqual(members[0].kind, "application")
        self.assertEqual((self.destination / "bin/server").stat().st_mode & 0o777, 0o555)


class PublicationTests(unittest.TestCase):
    setUp = PayloadTests.setUp
    inspect = PayloadTests.inspect

    def test_immutable_publication_bundle_mapping_and_tampered_existing_tree(self):
        artifact, content = bundle_fixture()
        members = self.inspect(artifact, content)
        payload = ArtifactPayload("extension", "linux-amd64", "deploy/artifacts/extension.tar.gz", checksum(content), members)
        host = Host(Paths(self.root))
        host.paths.opt("sample-test").mkdir(parents=True)
        tree = a.publish(host, "sample-test", artifact, payload, self.payload, a.ExpansionBudget(), history=(), observed_service_abi="service-1")
        self.addCleanup(lambda: a._remove_staging(tree))
        reference = a.ArtifactReference("extension", artifact.version, payload.platform, payload.sha256)
        self.assertEqual(tree, host.paths.opt("sample-test") / "artifacts" / reference.relative_path())
        export = a.bundle_export(reference, artifact, tree, "plugin", members, observed_service_abi="service-1", uid=os.geteuid())
        self.assertEqual(export.source, tree / "plugin")
        self.assertTrue(export.read_only)
        self.assertEqual(a.publish(host, "sample-test", artifact, payload, self.payload, a.ExpansionBudget(),
                                  history=(reference,), observed_service_abi="service-1"), tree)
        data = tree / "plugin/data.dat"
        data.chmod(0o644)
        data.write_bytes(b"tampered")
        data.chmod(0o444)
        with self.assertRaises(DeployError):
            a.publish(host, "sample-test", artifact, payload, self.payload, a.ExpansionBudget(), history=(reference,), observed_service_abi="service-1")
        self.assertEqual(data.read_bytes(), b"tampered")
        self.assertFalse(list(tree.parent.glob(".artifact-*")))

    def test_abi_unknown_or_different_refuses_and_version_history_survives_pruning(self):
        artifact, _ = bundle_fixture()
        for observed in (None, "service-2"):
            with self.assertRaises(DeployError):
                a.validate_abi(artifact, observed)
        a.validate_abi(artifact, "service-1")
        old = a.ArtifactReference("extension", "v1", "linux-amd64", "a" * 64)
        with self.assertRaises(DeployError):
            a.validate_history((replace(old, sha256="b" * 64),), (old,))
        a.validate_history((replace(old, version="v2", sha256="b" * 64),), (old,))

    def test_every_lifetime_reference_and_unknown_observation_retains_tree(self):
        reference = a.ArtifactReference("extension", "v1", "linux-amd64", "a" * 64)
        self.assertEqual(a.retention_reasons(reference, a.RetentionInputs()), ("unknown_observations",))
        self.assertEqual(a.retention_reasons(reference, a.RetentionInputs(observations_complete=True)), ())
        for field in ("retained_releases", "selections", "operations", "processes", "mounts", "backups"):
            self.assertEqual(a.retention_reasons(reference, a.RetentionInputs(**{field: (reference,), "observations_complete": True})), (field,))
