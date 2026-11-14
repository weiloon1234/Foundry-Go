"""Bounded value-only templates. No application code or general template engine."""
import json
import re
import unicodedata

from contracts import DeployError, require

TOKEN = re.compile(r"\{\{(secret\.[A-Z][A-Z0-9_]*_FILE|binding\.[a-z][a-z0-9_-]*\.(?:address|port|url|path))\}\}")
KEY = r"[A-Za-z_][A-Za-z0-9_.-]*"


def _require(condition):
    require(condition, "service_config_template", "Invalid template value, position, reference or encoding.")


def _references(value):
    remainder = TOKEN.sub("", value)
    _require("{{" not in remainder and "}}" not in remainder)
    return tuple(match[1] for match in TOKEN.finditer(value))


def _controls(value):
    return any(unicodedata.category(c) in ("Cc", "Cs", "Zl", "Zp") for c in value)


def _yaml_string(value):
    # YAML escapes Unicode scalars, not UTF-16 surrogate pairs. Keep non-BMP
    # characters as UTF-8 while escaping YAML's non-printable/line-break range.
    return re.sub("[\x7f-\x9f\u2028\u2029\ufffe\uffff]",
                  lambda match: "\\u%04x" % ord(match[0]), json.dumps(value, ensure_ascii=False))


def _interpolate(value, resolved, limit):
    parts, offset, size = [], 0, 0
    for match in TOKEN.finditer(value):
        part = value[offset:match.start()] + resolved[match[1]]
        size += len(part.encode("utf-8"))
        _require(size <= limit)
        parts.append(part)
        offset = match.end()
    parts.append(value[offset:])
    result = "".join(parts)
    _require(len(result.encode("utf-8")) <= limit)
    return result


def render(data, format, resolve, *, limit):
    """Interpolate only parsed values, then encode each complete scalar once.

    YAML deliberately accepts single-line block mappings/sequences, not the full
    language. Reject unsupported context before reading any private values.
    """
    _require(len(data) <= limit and format in ("json", "yaml", "ini", "text"))
    try:
        source = data.decode("utf-8")
        _require(not any(unicodedata.category(c) in ("Cs", "Zl", "Zp") for c in source))
        values = []
        if format == "json":
            def pairs(items):
                _require(len({k for k, _ in items}) == len(items))
                for key, _ in items:
                    _require(not _references(key))
                return dict(items)
            document = json.loads(source, object_pairs_hook=pairs,
                                  parse_constant=lambda _: _require(False))
            def visit(value):
                if isinstance(value, str):
                    values.append(value)
                elif isinstance(value, dict):
                    for item in value.values():
                        visit(item)
                elif isinstance(value, list):
                    for item in value:
                        visit(item)
            visit(document)
        elif format == "text":
            values = [source]
        else:
            lines = []
            for line in source.splitlines(keepends=True):
                _require("\r" not in line and not _controls(line.removesuffix("\n")))
                body = line.removesuffix("\n")
                stripped = body.strip()
                if not stripped or stripped.startswith("#") or format == "ini" and stripped.startswith(";"):
                    _require(not _references(body))
                    lines.append((line, None, ""))
                    continue
                if format == "ini":
                    if re.fullmatch(r"\[[^\[\]\r\n]+\]", stripped):
                        _require(not _references(body))
                        lines.append((line, None, ""))
                        continue
                    match = re.fullmatch(r"( *[^\s=\[\]#;][^=]*=)(.*)", body)
                else:
                    # No multiline/quoted keys, tags, anchors, aliases, directives
                    # or non-JSON flow syntax can change the meaning of later lines.
                    match = re.fullmatch(r"( *(?:- +)?" + KEY + r":(?: +|$))(.*)", body)
                    if match is None:
                        match = re.fullmatch(r"( *- +)(.*)", body)
                _require(match is not None and not _references(match[1]))
                prefix, value = match[1], match[2]
                refs = _references(value)
                if format == "yaml":
                    if refs:
                        _require(value == value.strip() and not value.startswith(('"', "'", "|", ">", "&", "*", "!", "[")))
                        # A template scalar is one unquoted value, including any
                        # literal URL suffix. The result is always double quoted.
                        _require(not re.search(r":\s|\s#", TOKEN.sub("placeholder", value)))
                    elif value:
                        if value.startswith(('"', "[", "{")):
                            json.loads(value)
                        elif value.startswith("'"):
                            _require(re.fullmatch(r"'(?:[^']|'')*'", value) is not None)
                        else:
                            _require(value[0] not in "|>&*!%@`]}?,:-" and not re.search(r":\s", value))
                values.append(value)
                lines.append((prefix, value, "\n" if line.endswith("\n") else ""))

        names = {name for value in values for name in _references(value)}
        _require(len(names) <= 128)
        resolved = {name: resolve(name) for name in sorted(names)}
        _require(all(isinstance(v, str) and not any(unicodedata.category(c) == "Cs" for c in v)
                     and len(v.encode("utf-8")) <= limit for v in resolved.values()))
        used = 0
        def substitute(value):
            nonlocal used
            result = _interpolate(value, resolved, limit)
            used += len((json.dumps(result, ensure_ascii=True) if format == "json" else result).encode("utf-8"))
            _require(used <= limit)
            return result
        if format == "json":
            def replace(value):
                if isinstance(value, str):
                    return substitute(value)
                if isinstance(value, dict):
                    return {key: replace(item) for key, item in value.items()}
                if isinstance(value, list):
                    return [replace(item) for item in value]
                return value
            output = json.dumps(replace(document), ensure_ascii=True, allow_nan=False, separators=(",", ":")) + "\n"
        elif format == "text":
            output = substitute(source)
            _require(not _controls(output))
        else:
            result, size = [], 0
            for prefix, value, ending in lines:
                if value is None or not _references(value):
                    line = prefix if value is None else prefix + value + ending
                else:
                    value = substitute(value)
                    if format == "ini":
                        # Raw INI has no portable quote/comment escaping, and a
                        # backslash can continue the line into the next setting.
                        _require(value == value.strip() and not _controls(value)
                                 and not any(c in value for c in "\\\"'#;"))
                    else:
                        value = _yaml_string(value)
                    line = prefix + value + ending
                size += len(line.encode("utf-8"))
                _require(size <= limit)
                result.append(line)
            output = "".join(result)
        encoded = output.encode("utf-8")
        _require(len(encoded) <= limit)
        return encoded
    except (UnicodeError, ValueError, TypeError, RecursionError):
        raise DeployError("service_config_template", "Template cannot be rendered safely.") from None
