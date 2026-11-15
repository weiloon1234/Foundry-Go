"""Public declaration inputs shared by offline validation and release/deploy."""
import os
from pathlib import PurePosixPath
import stat

from contracts import (MAX_JSON, check_path, parse_json, require, validate_profile,
                       validate_profile_identity)


def source_allowed(name):
    path = PurePosixPath(name)
    require(not path.is_absolute() and ".." not in path.parts and "\\" not in name
            and not any(ord(c) < 32 for c in name), "unsafe_path", "Source contains an unsafe path.")
    excluded = {".git", ".deploy", "dist", "bin", ".aws", ".ssh", ".netrc", ".npmrc", ".codex",
                "storage", "node_modules", "go.work", "go.work.sum", ".secrets", "secrets",
                "service-secrets", "copy-secrets", "secrets.json", "host-secrets.json",
                "service-secrets.json", "bootstrap-secrets.json"}
    # Case aliases must not expose private files on case-insensitive workspaces.
    normalized = name.lower()
    return not (any(part.lower() in excluded or part.lower().startswith(".env") for part in path.parts)
                or path.suffix.lower() in {".pem", ".key", ".p12", ".pfx", ".secret"}
                or normalized in {".claude/settings.local.json", "claude.local.md", "config/app.toml"}
                or normalized.startswith("public/build/")
                or normalized.startswith("config/") and normalized.endswith(".local.toml"))


def read_declaration(root, path, limit):
    require(path.is_relative_to(root) and source_allowed(path.relative_to(root).as_posix()),
            "unsafe_path", "Declaration must use a public application source path.")
    check_path(path, "file")
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
            "unsafe_path", "Declarations must be regular files without links.")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, "rb") as stream:
        current = os.fstat(stream.fileno())
        require(stat.S_ISREG(current.st_mode) and current.st_nlink == 1
                and (current.st_dev, current.st_ino) == (info.st_dev, info.st_ino),
                "unsafe_path", "Declaration changed before it could be read safely.")
        content = stream.read(limit + 1)
    require(len(content) <= limit, "declaration_size", "Declaration file exceeds its size bound.")
    return content


def read_declaration_json(root, path):
    return parse_json(read_declaration(root, path, MAX_JSON))


def load_profile(root, environment, project):
    """Release/deploy sibling checks also apply when reporting just one profile."""
    path = root / "deploy/environments" / (environment + ".json")
    profile = validate_profile(read_declaration_json(root, path), project)
    validate_profile_identity(profile, environment)
    for sibling in sorted(path.parent.glob("*.json")):
        if sibling != path:
            other = validate_profile(read_declaration_json(root, sibling), project)
            validate_profile_identity(other, sibling.stem, (profile,))
    return profile
