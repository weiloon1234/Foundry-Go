"""Verified HTTPS requests to the disposable host's fixture nginx sites."""

import http.client
from pathlib import Path
import socket
import ssl


def https_response(entry, path, byte_limit=65536):
    domain = entry["profile"]["domain"]
    certificate = Path("/etc/foundry/letsencrypt/live") / domain / "fullchain.pem"
    context = ssl.create_default_context(cafile=str(certificate))
    raw = socket.create_connection(("127.0.0.1", 443), timeout=5)
    connection = http.client.HTTPConnection(domain, 443, timeout=5)
    try:
        connection.sock = context.wrap_socket(raw, server_hostname=domain)
        connection.request("GET", path, headers={"Host": domain, "Connection": "close"})
        response = connection.getresponse()
        payload = response.read(byte_limit + 1)
        assert len(payload) <= byte_limit, "Fixture HTTPS response exceeded its bound"
        return response.status, response.headers, payload
    finally:
        connection.close()
        raw.close()
