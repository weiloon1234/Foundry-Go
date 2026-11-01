#!/usr/bin/env python3
"""Public build input and release packaging, shared by both target architectures."""
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tarfile


def public_env(request, destination):
    # The browser build's public-env guard remains the authoritative allowlist.
    lines = []
    for key, value in request["profile"]["public_env"].items():
        if not re.fullmatch(r"VITE_[A-Z0-9_]+", key) or not isinstance(value, str):
            raise ValueError("invalid public build setting")
        if any(char in value for char in "\r\n\x00$"):
            raise ValueError("public build values must be literal single-line strings")
        # Literal dotenv quoting, not JSON escaping: the existing Vite guard
        # checks that every value occurs unchanged on its assignment line.
        # Double-quoted dotenv values expand literal \\n/\\r sequences. Prefer
        # the literal delimiters, falling back only when decoding stays exact.
        quote = next((delimiter for delimiter in ("'", "`", '"')
                      if delimiter not in value and (delimiter != '"' or not any(
                          escape in value for escape in ("\\n", "\\r")))), None)
        if quote is None:
            raise ValueError("public build value cannot be represented literally with dotenv quoting")
        lines.append(f"{key}={quote}{value}{quote}\n")
    Path(destination).write_text("".join(lines), encoding="utf-8")


def package(request, stage, output):
    stage, output = Path(stage), Path(output)
    manifest = dict(request["manifest"])
    files = {}
    for path in sorted(stage.rglob("*")):
        relative = path.relative_to(stage).as_posix()
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError(f"release contains a non-regular path: {relative}")
        allowed = relative == "artisan" or relative in {"config", "config/deploy.toml", "public", "private-source-maps"} or relative.startswith(("public/", "private-source-maps/"))
        if not allowed:
            raise ValueError(f"unexpected release path: {relative}")
        if any(part.startswith(".env") or part in {".git", "node_modules"} for part in path.relative_to(stage).parts):
            raise ValueError("release contains a forbidden private path")
        if path.suffix.lower() in {".pem", ".key", ".p12", ".pfx"}:
            raise ValueError("release contains key material")
        if path.is_file():
            with path.open("rb") as content:
                files[relative] = hashlib.file_digest(content, "sha256").hexdigest()
    if not {"artisan", "config/deploy.toml"} <= files.keys():
        raise ValueError("release lacks its binary or deployment configuration")
    manifest["files"] = files
    (stage / "manifest.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    output.mkdir(parents=True, exist_ok=True)
    archive = output / "app.tar.gz"
    if archive.exists() or archive.is_symlink():
        raise ValueError("release output already exists")
    temporary = output / "app.tar.gz.partial"
    try:
        with tarfile.open(temporary, "x:gz", format=tarfile.PAX_FORMAT) as tar:
            for path in sorted(stage.rglob("*")):
                info = tar.gettarinfo(str(path), path.relative_to(stage).as_posix())
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mode = 0o755 if path.is_dir() or info.name == "artisan" else 0o644
                if info.isfile():
                    with path.open("rb") as content:
                        tar.addfile(info, content)
                else:
                    tar.addfile(info)
        os.replace(temporary, archive)
        (output / "manifest.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    finally:
        temporary.unlink(missing_ok=True)
    return archive


if __name__ == "__main__":
    request = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
    if sys.argv[1] == "public-env":
        public_env(request, sys.argv[3])
    elif sys.argv[1] == "package":
        package(request, sys.argv[3], sys.argv[4])
    else:
        raise SystemExit("unknown build metadata operation")
