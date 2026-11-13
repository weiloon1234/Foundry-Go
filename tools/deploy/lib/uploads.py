"""Opt-in upload ingress: isolated sized volumes, runtime values and nginx policy.

The administrator supplies dedicated mounted filesystems. Planning never formats,
mounts or resizes storage. Apply creates only owned runtime children after checking
mount identity/capacity; all processes share the declared per-instance disk budget.
"""
from pathlib import Path
import json
import os
import re
import stat

from contracts import check_path, fields, require
from edge import nginx_variable

POOL_NAMES = ("proxy", "request", "attachment", "staging", "small")
LIMIT_FIELDS = ("body_bytes", "file_bytes", "request_slots", "proxy_slots", "attachment_slots",
                "staging_slots", "staging_object_bytes", "request_total_seconds", "attachment_total_seconds",
                "storage_total_seconds", "proxy_read_idle_seconds", "client_body_idle_seconds",
                "proxy_send_idle_seconds", "send_idle_seconds", "connect_seconds")
# These are owned even when disabled: generic config cannot enable only one half
# of the proxy/application resource contract.
RUNTIME_KEYS = frozenset({
    "APP__HTTP__UPLOADS__ENABLED", "APP__HTTP__UPLOADS__SMALL_DIRECTORY",
    "APP__HTTP__UPLOADS__REQUEST__DIRECTORY", "APP__HTTP__UPLOADS__REQUEST__MAX_ACTIVE",
    "APP__HTTP__UPLOADS__REQUEST__MAX_BYTES", "APP__HTTP__UPLOADS__BODY_BYTES",
    "APP__HTTP__UPLOADS__FILE_BYTES", "APP__HTTP__UPLOADS__TIMEOUT",
    "APP__HTTP__UPLOADS__ATTACHMENT_MAX_ACTIVE", "APP__HTTP__UPLOADS__STORAGE_TIMEOUT",
    "APP__HTTP__UPLOADS__STAGING__DIRECTORY", "APP__HTTP__UPLOADS__STAGING__MAX_ACTIVE",
    "APP__HTTP__UPLOADS__STAGING__MAX_BYTES",
    "APP__FEATURES__ATTACHMENTS__CONFIG__TEMP_DIRECTORY",
    "APP__FEATURES__ATTACHMENTS__CONFIG__TIMEOUT",
})


def validate(value):
    fields(value, "schema routes pools " + " ".join(LIMIT_FIELDS), "upload ingress")
    require(type(value["schema"]) is int and value["schema"] == 1,
            "invalid_uploads", "Upload ingress requires schema 1.")
    require(isinstance(value["routes"], list) and 1 <= len(value["routes"]) <= 32
            and all(isinstance(route, str) and re.fullmatch(r"/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+/?", route)
                    and not route.startswith("/.well-known/") for route in value["routes"])
            and len(set(value["routes"])) == len(value["routes"]),
            "invalid_uploads", "Declare unique literal upload paths (no regex, variables or query strings).")
    require(all(type(value[key]) is int and 0 < value[key] <= (1 << 40) for key in LIMIT_FIELDS),
            "invalid_uploads", "Upload limits must be positive bounded integers.")
    require(value["file_bytes"] < value["body_bytes"] and value["file_bytes"] <= value["staging_object_bytes"]
            and all(value[key] <= 1024 for key in ("request_slots", "proxy_slots", "attachment_slots", "staging_slots"))
            and value["storage_total_seconds"] <= value["attachment_total_seconds"] <= value["request_total_seconds"] <= 86400
            and value["request_total_seconds"] < value["proxy_read_idle_seconds"] <= 86460
            and all(value[key] <= 3600 for key in ("client_body_idle_seconds", "proxy_send_idle_seconds", "send_idle_seconds", "connect_seconds")),
            "invalid_uploads", "Upload byte, concurrency and total/idle deadline relationships are invalid.")
    fields(value["pools"], " ".join(POOL_NAMES), "upload volumes")
    minimum = minimum_bytes(value)
    for name, size in value["pools"].items():
        require(type(size) is int and minimum[name] < size <= 1 << 50,
                "invalid_uploads", "Each sized volume must include reserve above its admitted byte budget.")
    return value


def minimum_bytes(value):
    return {"proxy": value["proxy_slots"] * 32768,
            "request": value["request_slots"] * value["body_bytes"],
            "attachment": value["attachment_slots"] * value["file_bytes"],
            "staging": value["staging_slots"] * value["staging_object_bytes"], "small": 2 << 20}


def volume_paths(entry):
    # Outside the app-owned data tree: privileged provisioning never traverses
    # application-mutable parents. One exact mount per pool, no bind aliases.
    base = Path("/var/lib/foundry/uploads") / entry["name"]
    return {name: base / name for name in POOL_NAMES}


def runtime_values(entry):
    value = entry["profile"].get("uploads")
    if value is None:
        return {}
    validate(value)
    paths = volume_paths(entry)
    return {
        "TRANSPORT__UPLOAD_DIRECTORY": str(paths["small"] / "data"),
        "APP__HTTP__UPLOADS__ENABLED": "true",
        "APP__HTTP__UPLOADS__SMALL_DIRECTORY": str(paths["small"] / "data"),
        "APP__HTTP__UPLOADS__REQUEST__DIRECTORY": str(paths["request"] / "data"),
        "APP__HTTP__UPLOADS__REQUEST__MAX_ACTIVE": str(value["request_slots"]),
        "APP__HTTP__UPLOADS__REQUEST__MAX_BYTES": str(value["request_slots"] * value["body_bytes"]),
        "APP__HTTP__UPLOADS__BODY_BYTES": str(value["body_bytes"]),
        "APP__HTTP__UPLOADS__FILE_BYTES": str(value["file_bytes"]),
        "APP__HTTP__UPLOADS__TIMEOUT": str(value["request_total_seconds"]) + "s",
        "APP__HTTP__UPLOADS__ATTACHMENT_MAX_ACTIVE": str(value["attachment_slots"]),
        "APP__HTTP__UPLOADS__STORAGE_TIMEOUT": str(value["storage_total_seconds"]) + "s",
        "APP__HTTP__UPLOADS__STAGING__DIRECTORY": str(paths["staging"] / "data"),
        "APP__HTTP__UPLOADS__STAGING__MAX_ACTIVE": str(value["staging_slots"]),
        "APP__HTTP__UPLOADS__STAGING__MAX_BYTES": str(value["staging_slots"] * value["staging_object_bytes"]),
        "APP__FEATURES__ATTACHMENTS__CONFIG__TEMP_DIRECTORY": str(paths["attachment"] / "data"),
        "APP__FEATURES__ATTACHMENTS__CONFIG__TIMEOUT": str(value["attachment_total_seconds"]) + "s",
    }


def inspect(host, entry, *, runtime=False):
    value = entry["profile"].get("uploads")
    if value is None:
        return {}
    validate(value)
    paths, devices, result = volume_paths(entry), set(), {}
    for name, path in paths.items():
        local = check_path(host.paths.p(str(path)), "directory")
        require(local.is_dir(), "upload_storage_missing", "Provision every dedicated upload volume before planning.")
        # Root-controlled mount parents are essential; an app cannot replace a
        # checked mount or redirect the subsequent install/chown through symlinks.
        for parent in (local, *list(local.parents)[:-1]):
            info = parent.stat()
            require(stat.S_ISDIR(info.st_mode) and not info.st_mode & 0o022
                    and (host.paths.root != Path("/") or info.st_uid == 0),
                    "upload_storage_ownership", "Upload mount parents must be root-controlled directories.")
            if parent == host.paths.root:
                break
        mounted = host.run(["findmnt", "--json", "--mountpoint", str(local), "--output", "TARGET,SOURCE,FSTYPE,OPTIONS,MAJ:MIN"], check=False)
        try:
            records = json.loads(mounted.stdout)["filesystems"]
            mount = records[0]
            valid = mounted.returncode == 0 and len(records) == 1 and mount["target"] == str(local)
        except (ValueError, KeyError, IndexError, TypeError):
            valid = False
        require(valid, "upload_storage_mount", "Each upload pool requires its own mounted filesystem.")
        options = set(mount["options"].split(","))
        require(mount["fstype"] in ("ext4", "xfs") and "rw" in options and "ro" not in options
                and "bind" not in options and "[" not in mount["source"] and mount["maj:min"] not in devices,
                "upload_storage_isolation", "Upload pools require distinct writable ext4/xfs volumes, not directory or bind aliases.")
        devices.add(mount["maj:min"])
        usage = os.statvfs(local)
        size = usage.f_blocks * usage.f_frsize
        writable_capacity = (usage.f_blocks - usage.f_bfree + usage.f_bavail) * usage.f_frsize
        minimum = minimum_bytes(value)[name]
        if name in ("attachment", "staging"):
            # Every declared service can publish/import. Budgets include each
            # process, rather than treating one manager's limit as host-global.
            minimum *= len(entry["topology"]["roles"])
        require(minimum < writable_capacity and size <= value["pools"][name],
                "upload_storage_capacity", "Mounted capacity must include admission headroom and not exceed the declared hard quota.")
        require(not usage.f_flag & os.ST_RDONLY, "upload_storage_readonly", "Upload volume is read-only.")
        owner = "www-data" if name == "proxy" else entry["name"]
        child = check_path(local / "data", "directory")
        if runtime or child.exists():
            _runtime_directory(host, child, owner)
        result[name] = {"mount": str(path), "device": mount["maj:min"], "source": mount["source"],
                        "bytes": size, "writable_bytes": writable_capacity, "maximum_bytes": value["pools"][name], "owner": owner}
    return result


def _runtime_directory(host, child, owner):
    require(child.is_dir() and stat.S_IMODE(child.stat().st_mode) == 0o700,
            "upload_storage_ownership", "Upload runtime directories require mode 0700.")
    writable = host.run(["runuser", "-u", owner, "--", "test", "-O", str(child), "-a", "-w", str(child)], check=False)
    require(writable.returncode == 0, "upload_storage_ownership", "Upload runtime storage must be owned and writable by its service identity.")


def provision(host, entry):
    if "uploads" not in entry["profile"]:
        return
    inspect(host, entry)
    for name, path in volume_paths(entry).items():
        target = check_path(host.paths.p(str(path / "data")), "directory")
        owner = "www-data" if name == "proxy" else entry["name"]
        if target.exists():
            _runtime_directory(host, target, owner)
        else:
            host.run(["install", "-d", "-m", "0700", "-o", owner, "-g", owner, "--", str(target)])
    inspect(host, entry, runtime=True)


def service_paths(entry):
    if "uploads" not in entry["profile"]:
        return ""
    paths = volume_paths(entry)
    # ReadWritePaths explicitly includes every HTTP/worker/scheduler borrower.
    return "ReadWritePaths=" + " ".join(str(paths[name] / "data") for name in POOL_NAMES if name != "proxy")


def nginx(entry, proxy, template):
    value = entry["profile"].get("uploads")
    if value is None:
        return "", ""
    validate(value)
    key, zone = nginx_variable(entry, "upload"), nginx_variable(entry, "uploads")
    admission = (f'map $server_name ${key} {{ default "{entry["profile"]["instance_id"]}"; }}\n'
                 f'limit_conn_zone ${key} zone={zone}:1m;')
    # Post-response discard must not inherit nginx's 30-second lingering default.
    lingering = min(5, value["request_total_seconds"])
    replacements = {"@UPLOAD_ZONE@": zone, "@UPLOAD_SLOTS@": str(value["proxy_slots"]),
                    "@UPLOAD_BYTES@": str(value["body_bytes"]),
                    "@UPLOAD_TEMP@": str(volume_paths(entry)["proxy"] / "data"),
                    "@UPLOAD_LINGERING_TIME@": str(lingering) + "s",
                    "@UPLOAD_LINGERING_TIMEOUT@": str(min(lingering, value["client_body_idle_seconds"])) + "s",
                    "@PORT@": str(entry["allocations"]["http_port"]),
                    "@UPLOAD_PROXY@": proxy.replace("@PROXY_READ_TIMEOUT@", str(value["proxy_read_idle_seconds"]) + "s")}
    for token, field in (("CLIENT_BODY", "client_body_idle_seconds"), ("PROXY_SEND", "proxy_send_idle_seconds"),
                         ("SEND", "send_idle_seconds"), ("CONNECT", "connect_seconds")):
        replacements["@UPLOAD_" + token + "@"] = str(value[field]) + "s"
    locations = []
    for route in value["routes"]:
        content = template.replace("@UPLOAD_LOCATION@", "= " + route)
        for token, replacement in replacements.items():
            content = content.replace(token, replacement)
        locations.append(content)
    locations.append('    location @foundry_upload_busy {\n        add_header Retry-After 1 always;\n        return 503;\n    }')
    return admission, "\n".join(locations)
