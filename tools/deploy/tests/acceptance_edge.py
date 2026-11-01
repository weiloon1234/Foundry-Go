"""Opt-in nginx real-IP/lockdown acceptance. No database, root or external network.

FOUNDRY_NGINX_EDGE_ACCEPTANCE=1 python3 -m unittest deploy/tests/acceptance_edge.py
Requires an already installed nginx. The milestone-gate owner runs this test.
"""

import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from provision import _template
from acceptance_host import fixtures, HOST_ID
from contracts import identity, marker


@unittest.skipUnless(os.environ.get("FOUNDRY_NGINX_EDGE_ACCEPTANCE") == "1", "milestone nginx gate only")
class NginxEdgeAcceptance(unittest.TestCase):
    def test_tcp_peer_controls_trust_and_lockdown(self):
        nginx = shutil.which("nginx")
        self.assertIsNotNone(nginx, "nginx must already be installed")

        class Echo(BaseHTTPRequestHandler):
            def do_GET(self):
                body = self.headers.get("X-Forwarded-For", "").encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_):
                pass

        upstream = ThreadingHTTPServer(("127.0.0.1", 0), Echo)
        thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(upstream.server_close)
        self.addCleanup(upstream.shutdown)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            tools = root / "tools"
            shutil.copytree(Path(__file__).resolve().parents[1] / "server", tools / "server")
            (tools / "lib").mkdir()
            # Model a Cloudflare TCP peer locally without trusting all loopback.
            (tools / "lib/cloudflare-ranges.json").write_text(json.dumps({
                "retrieved_at": time.strftime("%Y-%m-%d"), "ipv4": ["127.0.0.2/32"], "ipv6": ["2001:db8::/32"]}))
            acme = root / "acme/.well-known/acme-challenge"
            acme.mkdir(parents=True)
            (acme / "probe").write_text("challenge")
            for lockdown in (False, True):
                with self.subTest(lockdown=lockdown):
                    with socket.socket() as reservation:
                        reservation.bind(("127.0.0.1", 0))
                        port = reservation.getsockname()[1]
                    project, profile = fixtures()[0]
                    profile.update(domain="edge.example.invalid", edge_origin_lockdown=lockdown)
                    entry = identity(project, profile)
                    entry.update(profile=profile, host_id=HOST_ID)
                    entry["allocations"]["http_port"] = upstream.server_port
                    entry["topology"] = {"schema": 1, "roles": [{"name": "http"}]}
                    from topology import validate_topology
                    entry["topology"] = validate_topology(entry["topology"])
                    site = _template(tools, "nginx-http.conf", entry)
                    site = site.replace("listen 80;", f"listen 127.0.0.1:{port};").replace("    listen [::]:80;\n", "")
                    site = site.replace("/var/www/letsencrypt", str(root / "acme"))
                    configuration = root / "nginx.conf"
                    user = "user root;\n" if os.geteuid() == 0 else ""
                    configuration.write_text(f"{user}pid {root}/nginx.pid;\nerror_log {root}/error.log;\nevents {{}}\nhttp {{ access_log off; client_body_temp_path {root}/body; proxy_temp_path {root}/proxy; fastcgi_temp_path {root}/fastcgi; uwsgi_temp_path {root}/uwsgi; scgi_temp_path {root}/scgi;\n{site}\n}}\n")
                    process = subprocess.Popen([nginx, "-p", str(root), "-c", str(configuration), "-g", "daemon off;"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    try:
                        deadline = time.monotonic() + 5
                        while True:
                            try:
                                with socket.create_connection(("127.0.0.1", port), timeout=.1):
                                    break
                            except OSError:
                                if process.poll() is not None or time.monotonic() >= deadline:
                                    self.fail("isolated nginx did not start")
                                time.sleep(.02)

                        def request(peer, path="/"):
                            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2, source_address=(peer, 0))
                            try:
                                connection.request("GET", path, headers={"Host": "edge.example.invalid", "CF-Connecting-IP": "203.0.113.9", "X-Forwarded-For": "192.0.2.99"})
                                response = connection.getresponse()
                                return response.status, response.read().decode()
                            finally:
                                connection.close()

                        self.assertEqual(request("127.0.0.2"), (200, "203.0.113.9"))
                        self.assertEqual(request("127.0.0.1"), (200, "127.0.0.1"))
                        self.assertEqual(request("127.0.0.2", "/.well-known/acme-challenge/probe"), (200, "challenge"))
                        for path in ("/", "/.well-known/acme-challenge/probe"):
                            if lockdown:
                                with self.assertRaises(http.client.RemoteDisconnected):
                                    request("127.0.0.3", path)
                            else:
                                self.assertEqual(request("127.0.0.3", path), (200, "127.0.0.3" if path == "/" else "challenge"))
                    finally:
                        process.terminate()
                        process.wait(timeout=5)


if __name__ == "__main__":
    unittest.main()
