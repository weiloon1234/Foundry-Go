"""Upload policies exercise real render/runtime derivation and isolated mount evidence."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, validate_profile
from host import Host, Paths
from provision import _template
from runtime_config import runtime_environment, validate_configuration_changes
from test_contracts import fixture
import uploads

TOOLS = Path(__file__).resolve().parents[1]


def profile():
    return {"schema": 1, "routes": ["/attachments/video", "/media/video"],
            "body_bytes": 520 << 20, "file_bytes": 512 << 20,
            "request_slots": 4, "proxy_slots": 4, "attachment_slots": 2,
            "staging_slots": 2, "staging_object_bytes": 1 << 30,
            "request_total_seconds": 1800, "attachment_total_seconds": 1200,
            "storage_total_seconds": 900, "proxy_read_idle_seconds": 1860,
            "client_body_idle_seconds": 60, "proxy_send_idle_seconds": 60,
            "send_idle_seconds": 60, "connect_seconds": 10,
            "pools": {"proxy": 64 << 20, "request": 3 << 30, "attachment": 4 << 30,
                      "staging": 7 << 30, "small": 256 << 20}}


class UploadPolicyTests(unittest.TestCase):
    def setUp(self):
        self.project, self.profile, self.entry = fixture()
        self.profile["uploads"] = profile()

    def test_schema_bounds_and_deadlines(self):
        validate_profile(self.profile, self.project)
        for key, value in (("request_total_seconds", 0), ("proxy_read_idle_seconds", 120),
                           ("file_bytes", 521 << 20), ("request_slots", True),
                           ("routes", ["/uploads;return 200"]), ("routes", ["/x", "/x"])):
            with self.subTest(key=key):
                changed = copy.deepcopy(self.profile)
                changed["uploads"][key] = value
                with self.assertRaises(DeployError):
                    validate_profile(changed)
        changed = profile()
        changed["pools"]["request"] = 4 * changed["body_bytes"]
        with self.assertRaises(DeployError):
            uploads.validate(changed)

    def test_http_and_https_share_exact_unbuffered_policy(self):
        http = _template(TOOLS, "nginx-http.conf", self.entry)
        https = _template(TOOLS, "nginx-https.conf", self.entry)
        for rendered in (http, https):
            self.assertIn("client_max_body_size 25m;", rendered)
            self.assertIn("proxy_read_timeout 120s;", rendered)
            self.assertEqual(1, rendered.count("limit_conn_zone"))
            for route in self.profile["uploads"]["routes"]:
                policy = rendered.split("location = " + route + " {", 1)[1].split("\n    }", 1)[0]
                for directive in ("client_max_body_size 545259520;", "proxy_http_version 1.1;",
                                  "proxy_request_buffering off;", "proxy_buffering off;",
                                  "proxy_next_upstream off;", "proxy_read_timeout 1860s;",
                                  "client_body_timeout 60s;", "proxy_send_timeout 60s;",
                                  "send_timeout 60s;", "proxy_connect_timeout 10s;", "limit_conn_status 503;"):
                    self.assertIn(directive, policy)
                self.assertNotIn("120s", policy)
            self.assertIn("add_header Retry-After 1 always;", rendered)
            self.assertNotIn("@UPLOAD", rendered)
            self.assertNotIn("@PROXY", rendered)
        http_policy = http.split("location = /attachments/video {", 1)[1].split("\n    }", 1)[0]
        https_policy = https.split("location = /attachments/video {", 1)[1].split("\n    }", 1)[0]
        self.assertEqual(http_policy, https_policy)

    def test_disabled_profile_preserves_ordinary_behavior(self):
        del self.profile["uploads"]
        rendered = _template(TOOLS, "nginx-http.conf", self.entry)
        self.assertNotIn("limit_conn", rendered)
        self.assertNotIn("proxy_request_buffering off", rendered)
        self.assertNotIn("lingering_", rendered)
        self.assertIn("proxy_read_timeout 120s;", rendered)
        self.assertEqual({}, uploads.runtime_values(self.entry))

    def test_upload_lingering_is_derived_and_scoped_to_upload_locations(self):
        for total, idle, lingering, timeout in ((1, 60, 1, 1), (3, 2, 3, 2),
                                                (6, 60, 5, 5), (1800, 60, 5, 5)):
            with self.subTest(total=total, idle=idle):
                value = self.profile['uploads']
                value['request_total_seconds'] = total
                value['attachment_total_seconds'] = min(total, 1200)
                value['storage_total_seconds'] = min(total, 900)
                value['proxy_read_idle_seconds'] = total + 60
                value['client_body_idle_seconds'] = idle
                uploads.validate(value)
                for template in ('nginx-http.conf', 'nginx-https.conf'):
                    rendered = _template(TOOLS, template, self.entry)
                    ordinary = rendered
                    for route in value['routes']:
                        policy = rendered.split('location = ' + route + ' {', 1)[1].split('\n    }', 1)[0]
                        self.assertIn(f'lingering_time {lingering}s;', policy)
                        self.assertIn(f'lingering_timeout {timeout}s;', policy)
                        ordinary = ordinary.replace(policy, '')
                    self.assertNotIn('lingering_', ordinary)

    def test_runtime_paths_and_sandbox_are_shared_owners(self):
        values = runtime_environment(self.entry, {"database_password": "fixture", "redis_password": "fixture"})
        paths = uploads.volume_paths(self.entry)
        self.assertEqual(str(paths["request"] / "data"), values["APP__HTTP__UPLOADS__REQUEST__DIRECTORY"])
        self.assertEqual(str(paths["attachment"] / "data"), values["APP__FEATURES__ATTACHMENTS__CONFIG__TEMP_DIRECTORY"])
        self.assertEqual(str(paths["small"] / "data"), values["TRANSPORT__UPLOAD_DIRECTORY"])
        self.assertEqual(str(4 * (520 << 20)), values["APP__HTTP__UPLOADS__REQUEST__MAX_BYTES"])
        self.assertEqual("1800s", values["APP__HTTP__UPLOADS__TIMEOUT"])
        for role in ("http", "worker", "scheduler"):
            unit = _template(TOOLS, f"foundry-{role}@.service", self.entry)
            self.assertIn("ProtectSystem=strict", unit)
            self.assertIn(uploads.service_paths(self.entry), unit)
        for key in uploads.RUNTIME_KEYS:
            with self.assertRaises(DeployError):
                validate_configuration_changes({key: "override"}, self.entry)
            with self.assertRaises(DeployError):
                validate_configuration_changes({key + "_FILE": "override"}, self.entry)

    def test_separate_mounts_capacity_permissions_and_retained_occupancy(self):
        with tempfile.TemporaryDirectory() as temporary:
            host = Host(Paths(Path(temporary)))
            paths = uploads.volume_paths(self.entry)
            for path in paths.values():
                host.paths.p(str(path)).mkdir(parents=True, mode=0o755)
                (host.paths.p(str(path)) / "data").mkdir(mode=0o700)
            devices = {str(host.paths.p(str(path))): str(i) for i, path in enumerate(paths.values())}
            sizes = {str(host.paths.p(str(path))): self.profile["uploads"]["pools"][name] for name, path in paths.items()}
            read_only, same_device, writable, mounted = False, False, True, True
            reserve_half = False
            def run(args, **kwargs):
                if args[0] == "findmnt":
                    path = args[3]
                    record = {"target": path, "source": "/dev/volume" + devices[path], "fstype": "ext4",
                              "options": "ro" if read_only else "rw", "maj:min": "0" if same_device else devices[path]}
                    return subprocess.CompletedProcess(args, 0, json.dumps({"filesystems": [record] if mounted else []}), "")
                self.assertEqual("runuser", args[0])
                return subprocess.CompletedProcess(args, 0 if writable else 1, "", "")
            def statvfs(path):
                # Zero free bytes does not erase occupancy or increase any quota.
                return SimpleNamespace(f_blocks=sizes[str(path)], f_frsize=1, f_flag=0, f_bavail=0, f_bfree=sizes[str(path)] // 2 if reserve_half else 0)
            with patch.object(host, "run", side_effect=run), patch.object(uploads.os, "statvfs", side_effect=statvfs):
                observed = uploads.inspect(host, self.entry, runtime=True)
                self.assertEqual(set(uploads.POOL_NAMES), set(observed))
                reserve_half = True
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)
                reserve_half = False
                read_only = True
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)
                read_only, same_device = False, True
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)
                same_device, writable = False, False
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry, runtime=True)
                writable, mounted = True, False
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)
                mounted = True
                request = str(host.paths.p(str(paths["request"])))
                sizes[request] += 1
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)
                sizes[request] = 4 * (520 << 20)
                with self.assertRaises(DeployError): uploads.inspect(host, self.entry)

    def test_provision_refuses_to_take_over_an_existing_foreign_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            host = Host(Paths(Path(temporary)))
            proxy = host.paths.p(str(uploads.volume_paths(self.entry)["proxy"] / "data"))
            proxy.mkdir(parents=True, mode=0o700)
            with patch.object(uploads, "inspect"), patch.object(host, "run", return_value=subprocess.CompletedProcess([], 1, "", "")) as run:
                with self.assertRaises(DeployError) as error:
                    uploads.provision(host, self.entry)
                self.assertEqual("upload_storage_ownership", error.exception.code)
                self.assertTrue(all(call.args[0][0] != "install" for call in run.call_args_list))
