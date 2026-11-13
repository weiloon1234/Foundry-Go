#!/usr/bin/env python3
"""Real B26 uploads, only inside the guarded disposable systemd container.

Use the installed fixture and production renderers. Do not run on an SSH host.
All injected files, mounts, markers and config copies belong to this phase.
"""
import concurrent.futures
from contextlib import contextmanager
import copy
import errno
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import sys
import threading
import time
import uuid

TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS / "lib"))
from acceptance_http import https_response
from acceptance_support import WORK, fixture, guard, run, service, wait_for
from contracts import atomic_write, check_path
from provision import _template
import release
from topology import instance_units
import uploads

MIB = 1 << 20
FILE_BYTES = 128 * MIB
PREFIX = b'--acceptance\r\nContent-Disposition: form-data; name="file"; filename="payload.txt"\r\nContent-Type: text/plain\r\n\r\n'
SUFFIX = b'\r\n--acceptance--\r\n'
BLOCK = b'x' * (64 << 10)


def policy():
    return {"schema": 1, "routes": ["/acceptance/uploads/large", "/acceptance/uploads/short"],
            "body_bytes": 129 * MIB, "file_bytes": FILE_BYTES,
            "proxy_slots": 3, "request_slots": 4, "attachment_slots": 3,
            "staging_slots": 2, "staging_object_bytes": 512 * MIB,
            "request_total_seconds": 300, "attachment_total_seconds": 240,
            "storage_total_seconds": 210, "proxy_read_idle_seconds": 360,
            "client_body_idle_seconds": 3, "proxy_send_idle_seconds": 60,
            "send_idle_seconds": 60, "connect_seconds": 10,
            "pools": {"proxy": 32 * MIB, "request": 768 * MIB,
                      "attachment": 2 << 30, "staging": 4 << 30, "small": 32 * MIB}}


def connection(entry, direct=False):
    if direct:
        return http.client.HTTPConnection("127.0.0.1", entry["allocations"]["http_port"], timeout=330)
    domain = entry["profile"]["domain"]
    context = ssl.create_default_context(cafile=str(Path("/etc/foundry/letsencrypt/live") / domain / "fullchain.pem"))
    raw = socket.create_connection(("127.0.0.1", 443), timeout=330)
    conn = http.client.HTTPConnection(domain, 443, timeout=330)
    try:
        conn.sock = context.wrap_socket(raw, server_hostname=domain)
    except BaseException:
        raw.close()
        raise
    return conn


def request(entry, path, method="GET", direct=False):
    conn = connection(entry, direct)
    try:
        conn.request(method, path, headers={"Connection": "close"})
        response = conn.getresponse()
        payload = response.read(65537)
        assert len(payload) <= 65536, "Unbounded fixture response"
        return response.status, json.loads(payload) if payload else None
    finally:
        conn.close()


def upload(entry, identifier, *, size=FILE_BYTES, chunked=False, hold="", kind="large",
           direct=False, delay=0, stall=0, disconnect=False, headers_received=None):
    conn = connection(entry, direct)
    started = time.monotonic()
    try:
        conn.putrequest("POST", f"/acceptance/uploads/{kind}?id={identifier}&hold={hold}")
        conn.putheader("Content-Type", "multipart/form-data; boundary=acceptance")
        conn.putheader("Connection", "close")
        conn.putheader("Transfer-Encoding" if chunked else "Content-Length",
                       "chunked" if chunked else str(len(PREFIX) + size + len(SUFFIX)))
        conn.endheaders()
        def send(data):
            conn.send((f"{len(data):x}\r\n".encode() + data + b"\r\n") if chunked else data)
        send(PREFIX)
        if stall:
            send(BLOCK[:1])
            time.sleep(stall)
            # Read the server's timeout without sending the rest of the body.
        elif disconnect:
            send(BLOCK)
            return {"status": 0, "elapsed": time.monotonic() - started}
        else:
            remaining = size
            while remaining:
                data = BLOCK[:min(len(BLOCK), remaining)]
                send(data)
                remaining -= len(data)
                if delay:
                    time.sleep(delay)
            send(SUFFIX)
            if chunked:
                conn.send(b"0\r\n\r\n")
        response = conn.getresponse()
        if headers_received is not None:
            headers_received.set()
        body = response.read(65537)
        assert len(body) <= 65536
        return {"status": response.status, "body": json.loads(body) if body and response.getheader("Content-Type", "").startswith("application/json") else None,
                "elapsed": time.monotonic() - started}
    except (BrokenPipeError, ConnectionResetError, http.client.RemoteDisconnected):
        return {"status": 0, "elapsed": time.monotonic() - started}
    finally:
        conn.close()


def occupancy(root):
    total = 0
    for path in root.rglob("*"):
        try:
            if path.is_file():
                total += path.stat().st_size
        except FileNotFoundError:
            pass  # The application can unlink a finished capture during sampling.
    return total


class Monitor:
    def __init__(self, paths):
        self.paths, self.peaks = paths, {name: 0 for name in paths}
        self.stop = threading.Event()
        self.error = None
        self.thread = threading.Thread(target=self.sample, daemon=True)

    def sample(self):
        try:
            while not self.stop.is_set():
                for name, path in self.paths.items():
                    self.peaks[name] = max(self.peaks[name], occupancy(path / "data"))
                self.stop.wait(0.01)
        except BaseException as error:
            self.error = error

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *args):
        self.stop.set()
        self.thread.join(timeout=5)
        assert not self.thread.is_alive(), "Disk sampler did not drain"
        if self.error:
            raise self.error
        assert self.peaks["proxy"] < MIB, "Nginx spooled the request body"
        for name, limit in policy()["pools"].items():
            assert self.peaks[name] <= limit, f"{name} exceeded its quota"
        print("UPLOAD_DISK " + json.dumps(self.peaks, sort_keys=True), flush=True)


@contextmanager
def configured_fixture():
    guard()
    host, _, _, original = fixture()
    entry = copy.deepcopy(original)
    entry["profile"]["uploads"] = policy()
    uploads.validate(policy())
    paths = uploads.volume_paths(entry)
    unit = instance_units(entry)["http"]
    fragment = Path(run(["systemctl", "show", unit, "-p", "FragmentPath", "--value"]).stdout.strip())
    assert fragment.parent == Path("/etc/systemd/system") and fragment.name == unit
    site = Path("/etc/nginx/sites-available") / ("foundry-" + entry["name"] + ".conf")
    check_path(fragment, "file")
    check_path(site, "file")
    original_unit, original_site = fragment.read_text(), site.read_text()
    assert original_site == _template(TOOLS, "nginx-https.conf", original), "Refusing drifted nginx site"
    registry = host.paths.registry.read_bytes()
    temporary = WORK / ("uploads-" + str(uuid.uuid4()))
    temporary.mkdir(mode=0o700)
    env_file = host.paths.instance_config(entry["name"]) / (temporary.name + ".env")
    assert not env_file.exists()
    mounted = []
    created_directories = []
    images = []
    changed = False
    try:
        for name, path in paths.items():
            assert not path.exists(), "Refusing an existing upload volume"
            # guard() uses umask 077. Only newly created root-controlled mount
            # parents become traversable; never chmod an existing parent.
            missing = []
            cursor = path
            while not cursor.exists():
                missing.append(cursor)
                cursor = cursor.parent
            check_path(cursor, "directory")
            for directory in reversed(missing):
                directory.mkdir(mode=0o755)
                directory.chmod(0o755)
                created_directories.append(directory)
            image = temporary / (name + ".ext4")
            images.append(image)
            run(["truncate", "-s", str(policy()["pools"][name]), str(image)])
            run(["mkfs.ext4", "-q", "-m", "0", "-F", str(image)])
            run(["mount", "-o", "loop,nodev,nosuid", str(image), str(path)])
            mounted.append(path)
        uploads.provision(host, entry)
        values = uploads.runtime_values(entry)
        original_env = host.paths.instance_config(entry["name"]) / "app.env"
        # Never print or mutate the working credentials. The private copy adds
        # only the exact upload keys, replacing any previous values in that copy.
        lines = [line for line in original_env.read_text().splitlines() if line.partition("=")[0] not in values]
        atomic_write(env_file, "\n".join(lines + [f"{key}={value}" for key, value in values.items()]) + "\n", mode=0o600)
        run(["chown", entry["name"] + ":" + entry["name"], str(env_file)])
        selected = host.paths.opt(entry["name"]) / "current"
        release.app(host, entry, selected, "migrate", env_file=env_file, timeout=120)
        run(["systemctl", "stop", unit])
        changed = True
        rendered = _template(TOOLS, "foundry-http@.service", entry,
                             postgres_unit=host.read_registry()["shared"]["postgresql"]["unit"])
        assert str(original_env) in rendered
        atomic_write(fragment, rendered.replace(str(original_env), str(env_file)))
        atomic_write(site, _template(TOOLS, "nginx-https.conf", entry))
        run(["nginx", "-t", "-q"])
        run(["systemctl", "daemon-reload"])
        run(["systemctl", "restart", unit])
        run(["systemctl", "reload", "nginx"])
        wait_for(lambda: request(entry, "/acceptance/uploads/usage")[0] == 200, "Upload fixture did not start")
        service(unit)
        yield host, entry, paths
    finally:
        if changed:
            run(["systemctl", "stop", unit])
            atomic_write(fragment, original_unit)
            atomic_write(site, original_site)
            run(["systemctl", "daemon-reload"])
            run(["nginx", "-t", "-q"])
            run(["systemctl", "restart", unit])
            run(["systemctl", "reload", "nginx"])
        env_file.unlink(missing_ok=True)
        for path in reversed(mounted):
            run(["umount", str(path)])
        for image in images:
            image.unlink(missing_ok=True)
        for directory in reversed(created_directories):
            directory.rmdir()
        temporary.rmdir()
        assert host.paths.registry.read_bytes() == registry, "Acceptance changed the registry"
        wait_for(lambda: https_response(original, "/health/ready")[0] == 200, "Original fixture did not recover")


class Scenarios:
    def __init__(self, host, entry, paths):
        self.host, self.entry, self.paths = host, entry, paths
        self.receipts = host.paths.data(entry["name"]) / "hostdrill"
        self.ids = []

    def identifier(self):
        value = str(uuid.uuid4())
        self.ids.append(value)
        return value

    def usage(self):
        status, body = request(self.entry, "/acceptance/uploads/usage")
        assert status == 200
        return body

    def release(self, identifier):
        marker = self.receipts / (identifier + ".release")
        marker.touch(mode=0o600, exist_ok=False)
        run(["chown", self.entry["name"] + ":" + self.entry["name"], str(marker)])

    def held(self, identifier):
        return (self.receipts / (identifier + ".held")).exists()

    def small(self):
        assert https_response(self.entry, "/health/live")[0] == 200, "Small JSON unavailable"
        assert upload(self.entry, self.identifier(), size=1024, kind="multipart")["status"] == 204, "Small multipart capture unavailable"
        identifier = self.identifier()
        result = upload(self.entry, identifier, size=1024, kind="small")
        self.success(identifier, result, 1024)

    def success(self, identifier, result, size):
        assert result["status"] == 200, f"Upload failed with status {result['status']}"
        digest = hashlib.sha256()
        remaining = size
        while remaining:
            data = BLOCK[:min(len(BLOCK), remaining)]
            digest.update(data)
            remaining -= len(data)
        assert result["body"]["bytes"] == size and result["body"]["sha256"] == digest.hexdigest()
        assert len(list(self.receipts.glob(identifier + ".put-*"))) == 1, "Duplicate storage publication"
        assert (self.receipts / (identifier + ".committed")).exists()
        operation = result["body"]["operation"]
        uuid.UUID(operation)
        from acceptance_support import postgres_settings, psql
        count = psql(postgres_settings(self.host, self.entry),
                     "SELECT count(*) FROM foundry_attachments WHERE id='" + operation + "' AND state='ready';").stdout.strip()
        assert count == "1", "Publication did not commit exactly one ready attachment"
        assert request(self.entry, "/acceptance/uploads/cleanup?id=" + identifier, "POST")[0] == 204

    def clean(self):
        wait_for(lambda: self.usage()["active"] == 0 and self.usage()["publishing"] == 0,
                 "Upload capacity was not released", timeout=30)
        assert self.usage()["reserved"] == 0
        assert occupancy(self.paths["attachment"] / "data") == 0, "Retained attachment spool"
        # A pool owns a tiny lock file even when no request is active.
        assert occupancy(self.paths["request"] / "data") < 4096
        assert not list((self.paths["staging"] / "data").rglob(".foundry-tmp-*")), "Retained local staging"

    def silent(self):
        identifier = self.identifier()
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor, Monitor(self.paths):
            headers_received = threading.Event()
            future = executor.submit(upload, self.entry, identifier, hold="publication", headers_received=headers_received)
            try:
                wait_for(lambda: self.held(identifier), "Storage publication did not begin", timeout=90)
                started = time.monotonic()
                while time.monotonic() - started < 150:
                    assert not future.done() and not headers_received.is_set(), "Premature response during silent publication"
                    self.small()
                    time.sleep(5)
                self.release(identifier)
                result = future.result(timeout=90)
                assert result["elapsed"] > 120
                self.success(identifier, result, FILE_BYTES)
            finally:
                if not (self.receipts / (identifier + ".release")).exists():
                    self.release(identifier)
        self.clean()

    def bodies(self):
        for chunked in (False, True):
            identifier = self.identifier()
            with Monitor(self.paths):
                self.success(identifier, upload(self.entry, identifier, chunked=chunked), FILE_BYTES)
            self.clean()

    def clients(self):
        # Progress every 100 ms is below the 3 s idle gap and within total time.
        for name, kwargs, success in (
                ("progress", {"size": 4 * MIB, "delay": 0.1, "chunked": True}, True),
                ("stall", {"stall": 5}, False),
                ("trickle", {"size": 8 * MIB, "delay": 0.2, "kind": "short", "chunked": True}, False),
                ("disconnect", {"disconnect": True}, False)):
            identifier = self.identifier()
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor, Monitor(self.paths):
                future = executor.submit(upload, self.entry, identifier, **kwargs)
                while not future.done():
                    self.small()
                    time.sleep(0.2)
                result = future.result()
            if success:
                self.success(identifier, result, kwargs["size"])
            else:
                assert result["status"] in (0, 408, 499, 500, 503, 504), name + " unexpectedly succeeded"
                assert not list(self.receipts.glob(identifier + ".put-*")), name + " entered publication"
                assert not (self.receipts / (identifier + ".committed")).exists()
                if name == "trickle":
                    assert 5 <= result["elapsed"] < 15, "Trickle did not hit the declared total deadline"
            self.clean()
            print("PASS upload-client=" + name, flush=True)

    def saturation(self, pool):
        count = policy()[pool + "_slots"]
        identifiers = [self.identifier() for _ in range(count)]
        direct = pool != "proxy"
        # Hold incomplete bodies for proxy/request admission; completed bodies
        # held in the adapter expose attachment and local-staging ownership.
        holding_body = pool in ("proxy", "request")
        with concurrent.futures.ThreadPoolExecutor(max_workers=count + 1) as executor, Monitor(self.paths):
            connections = []
            futures = []
            keepalive_stop = threading.Event()
            keepalive = None
            try:
                if holding_body:
                    for identifier in identifiers:
                        conn = connection(self.entry, direct)
                        conn.putrequest("POST", "/acceptance/uploads/large?id=" + identifier)
                        conn.putheader("Content-Type", "multipart/form-data; boundary=acceptance")
                        conn.putheader("Content-Length", str(FILE_BYTES + len(PREFIX) + len(SUFFIX)))
                        conn.endheaders()
                        conn.send(PREFIX + BLOCK)
                        connections.append(conn)
                    def progress():
                        while not keepalive_stop.wait(0.5):
                            for connection in connections:
                                try:
                                    connection.send(b'x')
                                except OSError:
                                    return
                    keepalive = threading.Thread(target=progress, daemon=True)
                    keepalive.start()
                    wait_for(lambda: self.usage()["active"] == count, "Request captures were not admitted", timeout=2)
                else:
                    for identifier in identifiers:
                        futures.append(executor.submit(upload, self.entry, identifier, direct=True,
                                                       hold="staging" if pool == "staging" else "publication"))
                    wait_for(lambda: all(self.held(identifier) for identifier in identifiers), "Pool did not saturate", timeout=90)
                self.small()
                rejected = self.identifier()
                # Headers only: the application must reject before reading capture.
                if holding_body:
                    conn = connection(self.entry, direct)
                    try:
                        conn.putrequest("POST", "/acceptance/uploads/large?id=" + rejected)
                        conn.putheader("Content-Type", "multipart/form-data; boundary=acceptance")
                        conn.putheader("Content-Length", str(FILE_BYTES))
                        conn.endheaders()
                        conn.sock.settimeout(5)
                        response = conn.getresponse()
                        assert response.status == 503, "Saturated ingress admitted another capture"
                    finally:
                        conn.close()
                else:
                    future = executor.submit(upload, self.entry, rejected, direct=True,
                                             hold="staging" if pool == "staging" else "publication")
                    # The manager rejects before the adapter; staging rejects before
                    # reading its source or reaching the EOF barrier.
                    futures.append(future)
                    identifiers.append(rejected)
                    time.sleep(1)
                    assert not self.held(rejected), pool + " limit is not wired to its runtime owner"
                    result = future.result(timeout=15)
                    assert result["status"] == 503, "Saturated owner did not reject overload"
                if pool != "staging":
                    assert not list(self.receipts.glob(rejected + ".put-*")), "Rejected request entered storage"
                else:
                    assert len(list(self.receipts.glob(rejected + ".put-*"))) == 1, "Staging gate was masked by another owner"
            finally:
                keepalive_stop.set()
                if keepalive:
                    keepalive.join(timeout=2)
                    assert not keepalive.is_alive(), "Saturation sender did not drain"
                for conn in connections:
                    conn.close()
                for identifier in self.ids:
                    if not (self.receipts / (identifier + ".release")).exists():
                        self.release(identifier)
                for identifier, future in zip(identifiers, futures):
                    result = future.result(timeout=90)
                    if result["status"] == 200:
                        self.success(identifier, result, FILE_BYTES)
        self.clean()
        self.small()

    def full(self, pool):
        root = self.paths[pool] / "data"
        fill = root / ("acceptance-full-" + str(uuid.uuid4()))
        # Write, rather than truncate a sparse file. The volume is an exact,
        # disposable ext4 mount owned by this phase, never a shared filesystem.
        try:
            with fill.open("xb", buffering=0) as stream:
                while True:
                    try:
                        stream.write(BLOCK)
                    except OSError as error:
                        assert error.errno in (errno.ENOSPC, errno.EDQUOT)
                        break
            assert os.statvfs(root).f_bavail == 0, "Volume was not exhausted"
            small_failure = None
            try:
                self.small()
            except AssertionError as error:
                small_failure = error
            identifier = self.identifier()
            result = upload(self.entry, identifier)
            if pool == "proxy":
                # Unbuffered forwarding needs no full-body proxy file even when
                # the residual proxy quota is exhausted.
                self.success(identifier, result, FILE_BYTES)
            else:
                assert result["status"] != 200, pool + " quota failure published an attachment"
                assert not (self.receipts / (identifier + ".committed")).exists()
        finally:
            fill.unlink(missing_ok=True)
        self.clean()
        identifier = self.identifier()
        self.success(identifier, upload(self.entry, identifier), FILE_BYTES)
        self.small()
        self.clean()
        if small_failure:
            raise small_failure

    def remove_markers(self):
        for identifier in self.ids:
            for path in self.receipts.glob(identifier + ".*"):
                assert path.is_file() and not path.is_symlink()
                path.unlink()


def main():
    failures = []
    with configured_fixture() as (host, entry, paths):
        scenarios = Scenarios(host, entry, paths)
        cases = [("silent-150s", scenarios.silent), ("known-and-chunked", scenarios.bodies),
                 ("slow-stalled-trickling-disconnected", scenarios.clients)]
        cases += [("saturation-" + pool, lambda pool=pool: scenarios.saturation(pool))
                  for pool in ("proxy", "request", "attachment", "staging")]
        cases += [("full-" + pool, lambda pool=pool: scenarios.full(pool))
                  for pool in ("proxy", "request", "attachment", "staging")]
        try:
            for name, scenario in cases:
                try:
                    scenario()
                    print("PASS upload=" + name, flush=True)
                except (AssertionError, TimeoutError) as error:
                    failures.append(name)
                    print("FAIL upload=" + name + " reason=" + str(error), flush=True)
                    scenarios.clean()
        finally:
            scenarios.remove_markers()
    assert not failures, "Upload acceptance failed: " + ", ".join(failures)


if __name__ == "__main__":
    main()
