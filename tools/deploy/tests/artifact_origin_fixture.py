"""Loopback TLS origin requiring an exact signed query, with no external DNS."""
from contextlib import contextmanager
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import socket
import ssl
import subprocess
import threading
from unittest.mock import Mock

PUBLIC = "93.184.216.34"
HOSTNAME = "downloads.example.org"
URL = "https://" + HOSTNAME + "/payload"
SIGNED_TARGET = "/signed?token=fixture-secret%2F%2b+value&part=1&part=2&empty="


@contextmanager
def https_origin(root, body=b"payload", *, target=SIGNED_TARGET):
    certificate, key, config = (root / name for name in ("cert.pem", "key.pem", "openssl.cnf"))
    config.write_text(f"[req]\nprompt=no\ndistinguished_name=dn\nx509_extensions=extensions\n"
                      f"[dn]\nCN={HOSTNAME}\n[extensions]\nsubjectAltName=DNS:{HOSTNAME}\n")
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                    "-config", str(config), "-keyout", str(key), "-out", str(certificate)],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
    requests = []

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *args):
            pass

        def do_GET(self):
            requests.append(self.path)
            if self.path == "/payload":
                self.send_response(302)
                self.send_header("Location", "https://" + HOSTNAME + target)
                content = b""
            elif self.path == target:
                self.send_response(200)
                content = body
            else:
                self.send_response(403)
                content = b""
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)

    server_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server_context.load_cert_chain(certificate, key)
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.socket = server_context.wrap_socket(server.socket, server_side=True)
    context = ssl.create_default_context(cafile=str(certificate))

    class Connection(http.client.HTTPSConnection):
        def __init__(self, hostname, address, timeout):
            assert hostname == HOSTNAME and address == PUBLIC
            super().__init__(hostname, timeout=timeout, context=context)

        def connect(self):
            raw = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            try:
                raw.settimeout(self.timeout)
                raw.connect(server.server_address)
                secured = self._context.wrap_socket(raw, server_hostname=self.host)
            except BaseException:
                raw.close()
                raise
            # Only the transport seam maps a public peer to loopback; production
            # address validation remains active. HTTP and TLS use real sockets.
            self.sock = Mock(wraps=secured)
            self.sock.getpeername.return_value = (PUBLIC, 443)

    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True)
    thread.start()
    try:
        yield Connection, requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
