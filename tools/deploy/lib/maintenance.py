"""Maintenance command inputs and the bounded, secret-safe application response."""
import ipaddress
import re
import unicodedata

from contracts import DeployError, parse_json, require

FIELDS = frozenset({"message", "retry", "allow", "except", "bypass"})


def utf8_size(value):
    try:
        return len(value.encode("utf-8"))
    except UnicodeError:
        return float("inf")


def controls(value):
    return any(unicodedata.category(char) == "Cc" for char in value)


def valid_rule(value):
    if not isinstance(value, str):
        return False
    method, separator, path = value.partition(" ")
    if separator:
        if not re.fullmatch(r"[A-Z]{1,16}", method):
            return False
    else:
        path = method
    if path == "/*":
        path = "/"
    elif path.endswith("/*"):
        path = path[:-2]
    if path == "/":
        return True
    return (path.startswith("/") and utf8_size(path) <= 1024
            and not any(char in path for char in "?#%\\") and not controls(path)
            and all(segment not in ("", ".", "..") for segment in path[1:].split("/")))


def arguments(action, values):
    """Validate the wire fields shared by local input and the SSH boundary."""
    code = "invalid_maintenance"
    require(isinstance(values, dict) and set(values) <= FIELDS, code, "Unknown maintenance parameter.")
    require(action == "down" or not values, code, "Maintenance parameters are only supported for down.")
    if "message" in values:
        message = values["message"]
        require(isinstance(message, str) and utf8_size(message) <= 512 and not controls(message),
                code, "MESSAGE must be UTF-8, at most 512 bytes, with no control characters.")
    if "retry" in values:
        require(type(values["retry"]) is int and 0 <= values["retry"] <= 86400,
                code, "RETRY must be whole seconds from 0 to 86400.")
    for field in ("allow", "except"):
        if field not in values:
            continue
        items = values[field]
        require(isinstance(items, list) and len(items) <= 64, code, f"{field.upper()} accepts up to 64 entries.")
        for item in items:
            if field == "except":
                require(valid_rule(item), code, "EXCEPT requires [METHOD ]/path[/*] rules.")
            else:
                try:
                    require(isinstance(item, str) and "%" not in item
                            and ("/" not in item or re.fullmatch(r"[0-9]{1,3}", item.split("/", 1)[1])),
                            code, "ALLOW requires IP addresses or CIDRs.")
                    ipaddress.ip_network(item, strict=False)
                except ValueError:
                    raise DeployError(code, "ALLOW requires IP addresses or CIDRs.") from None
    if "bypass" in values:
        require(values["bypass"] is True, code, "BYPASS must be yes when supplied.")
    extra = ["--format", "json"]
    for field in ("message", "retry"):
        if field in values:
            extra.extend(("--" + field, str(values[field])))
    for field in ("allow", "except"):
        for value in values.get(field, []):
            extra.extend(("--" + field, value))
    if values.get("bypass"):
        extra.append("--with-secret")
    return tuple(extra)


def run(host, entry, path, action, extra):
    import release

    code = "maintenance_unavailable"
    detail = (f"The active release could not run `artisan {action} --format json`; deploy a release built on "
              "Foundry-Go v0.20.0 or later that registers application.MaintenanceCommands() and sets maintenance.store.")
    try:
        result = release.app(host, entry, path, action, extra=extra, check=False, timeout=60)
        require(result.returncode == 0 and isinstance(result.stdout, (str, bytes)), code, detail)
        payload = result.stdout.encode("utf-8") if isinstance(result.stdout, str) else result.stdout
        require(len(payload) <= 4096, code, detail)
        value = parse_json(payload)
        require(isinstance(value, dict) and value.get("down") is (action == "down")
                and set(value) <= {"down", "bypass_path"}, code, detail)
        if "bypass_path" in value:
            require(action == "down" and "--with-secret" in extra
                    and isinstance(value["bypass_path"], str)
                    and re.fullmatch(r"/[A-Za-z0-9_-]{16,128}", value["bypass_path"]), code, detail)
        return value
    except (DeployError, OSError, TypeError, ValueError):
        raise DeployError(code, detail) from None
