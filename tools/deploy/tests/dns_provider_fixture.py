"""Loopback-only controlled Cloudflare API, shared by unit and container scenarios."""
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import copy
import json
import re
import socket
import threading
from urllib.parse import parse_qs, urlsplit
from unittest.mock import patch
import uuid

import dns


class Provider:
    def __init__(self):
        self.records = {}
        self.calls = []
        self.fault = None
        self.revision = 0

    def put(self, value, record_id=None):
        self.revision += 1
        record_id = record_id or uuid.uuid4().hex
        self.records[record_id] = {"id": record_id, "created_on": "2026-11-15T00:00:00Z", "tags": [],
                                   "settings": {}, **copy.deepcopy(value), "modified_on": str(self.revision)}
        return record_id

    def handler(self):
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.dispatch()

            do_POST = do_GET
            do_PATCH = do_GET
            do_DELETE = do_GET

            def dispatch(self):
                parsed = urlsplit(self.path)
                segments = parsed.path.strip("/").split("/")
                record_id = segments[3] if len(segments) == 4 else None
                method = self.command
                fixture.calls.append((method, record_id))
                body = None
                size = int(self.headers.get("Content-Length", 0))
                if size:
                    body = json.loads(self.rfile.read(size))
                fault = fixture.fault
                if method != "GET":
                    fixture.fault = None
                if isinstance(fault, tuple) and method != "GET":
                    return self.reply(*fault)
                if fault == "rate_limit" and method != "GET":
                    return self.reply(429, {"success": False, "errors": [{"code": 1015, "message": "Rate limited"}],
                                            "private": "DO-NOT-PRINT-RESPONSE"})
                if fault == "reject" and method != "GET":
                    return self.reply(403, {"success": False, "errors": [{"code": 10000, "message": "Authentication error"}]})
                if fault == "lost_before" and method != "GET":
                    return self.lose()
                if method in ("POST", "PATCH"):
                    value = {**fixture.records.get(record_id, {}), **body}
                    if not isinstance(value.get("comment", ""), str) or len(value.get("comment", "")) > 100:
                        return self.reply(400, {"success": False, "errors": [{"code": 9313,
                            "message": "DNS record comment exceeds the maximum length of 100 characters."}]})
                    name = value.get("name")
                    if not isinstance(name, str) or len(name) > 253 or not all(
                            re.fullmatch(r"[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?", label) for label in name.split(".")):
                        return self.reply(400, {"success": False, "errors": [{"code": 9000, "message": "Invalid DNS record name."}]})
                if method == "GET":
                    if record_id:
                        if record_id not in fixture.records:
                            return self.reply(404, {"success": False})
                        result = fixture.records[record_id]
                    else:
                        name = parse_qs(parsed.query).get("name", [None])[0]
                        result = [r for r in fixture.records.values() if r["name"] == name]
                elif method == "POST":
                    record_id = fixture.put(body)
                    result = fixture.records[record_id]
                elif record_id not in fixture.records:
                    return self.reply(404, {"success": False})
                elif method == "PATCH":
                    fixture.put({**fixture.records[record_id], **body}, record_id)
                    result = fixture.records[record_id]
                elif method == "DELETE":
                    del fixture.records[record_id]
                    result = {"id": record_id}
                else:
                    return self.reply(400, {"success": False})
                if fault == "lost_after" and method != "GET":
                    return self.lose()
                if fault == "third_state" and method == "PATCH":
                    fixture.put({**result, "content": "203.0.113.99"}, record_id)
                    return self.lose()
                self.reply(200, {"success": True, "result": result, "result_info": {"total_pages": 1}})

            def lose(self):
                self.close_connection = True
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()

            def reply(self, status, body):
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        return Handler


@contextmanager
def controlled_provider():
    fixture = Provider()
    server = ThreadingHTTPServer(("127.0.0.1", 0), fixture.handler())
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True)
    thread.start()
    try:
        with patch.object(dns, "API", f"http://127.0.0.1:{server.server_port}"):
            yield fixture
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
