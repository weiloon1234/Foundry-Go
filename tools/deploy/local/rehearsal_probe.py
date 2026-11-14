"""Bounded TCP/TLS probe, run only in a separate rehearsal peer container."""
import json
import socket
import ssl
import sys


def probe(address, port, hostname, tls, opened):
    try:
        connection = socket.create_connection((address, port), timeout=3)
    except (TimeoutError, ConnectionRefusedError):
        return {"ok": not opened, "connected": False}
    # Other routing/DNS failures do not prove a source-policy denial.
    with connection:
        if not opened:
            return {"ok": False, "connected": True}
        if tls == "edge":
            context = ssl.create_default_context(cafile="/ca.crt")
            with context.wrap_socket(connection, server_hostname=hostname):
                pass
        return {"ok": True, "connected": True, "tls_verified": tls == "edge"}


if __name__ == "__main__":
    try:
        result = probe(sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4], sys.argv[5] == "open")
    except (OSError, ValueError):
        result = {"ok": False, "error": "probe_unavailable"}
    print(json.dumps(result))
    sys.exit(0 if result["ok"] else 1)
