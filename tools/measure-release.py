#!/usr/bin/env python3
"""Measure a private release candidate on native macOS; never use local replaces.

All child commands are explicit argv, all mutable state is under a new output
directory, and no credentials or application services are needed. This script
collects evidence; it does not publish or mark a milestone accepted.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import signal
import shutil
import subprocess
import time

from security_scan import decode_stream, inspect_vulnerabilities


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")



def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def under(path, root):
    return path.resolve().is_relative_to(root.resolve())


def clean_environment(cache, module_cache, proxy, module_paths):
    # Do not inherit developer compiler/GC/architecture knobs or credentials.
    env = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR", "DEVELOPER_DIR", "LANG", "LC_ALL") if key in os.environ}
    env.update(GOENV="off", GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-mod=readonly",
               GOCACHE=str(cache), GOMODCACHE=str(module_cache),
               GOPROXY=proxy, GOSUMDB="sum.golang.org", GONOPROXY="none",
               GONOSUMDB=",".join(module_paths))
    return env


def process_sample(owner):
    # Keep only numeric RSS of our descendants; never retain other command lines.
    output = subprocess.run(["/bin/ps", "-axo", "pid=,ppid=,rss=,comm="], capture_output=True, text=True, check=True, timeout=5).stdout
    entries = []
    for line in output.splitlines():
        parts = line.split(None, 3)
        if len(parts) == 4:
            entries.append((int(parts[0]), int(parts[1]), int(parts[2]) * 1024, Path(parts[3]).name))
    family = {owner}
    while True:
        children = {pid for pid, parent, _, _ in entries if parent in family}
        if children <= family:
            break
        family |= children
    selected = [(rss, command) for pid, _, rss, command in entries if pid in family]
    return sum(rss for rss, _ in selected), max((rss for rss, command in selected if command == "compile"), default=0)


class Runner:
    def __init__(self, output, timeout):
        self.output, self.timeout, self.results = output, timeout, []

    def run(self, name, argv, cwd, env):
        stage = self.output / name
        stage.mkdir()
        started = time.monotonic()
        maximum_tree, maximum_compiler, timed_out = 0, 0, False
        process = None
        failure = None
        with (stage / "stdout.log").open("w") as stdout, (stage / "stderr.log").open("w") as stderr:
            try:
                process = subprocess.Popen(["/usr/bin/time", "-l", "-o", str(stage / "resource.txt"), *map(str, argv)],
                                           cwd=cwd, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
                while process.poll() is None:
                    tree, compiler = process_sample(process.pid)
                    maximum_tree, maximum_compiler = max(maximum_tree, tree), max(maximum_compiler, compiler)
                    if time.monotonic() - started > self.timeout:
                        timed_out = True
                        raise TimeoutError("command exceeded measurement timeout")
                    time.sleep(0.2)
            except BaseException as error:
                failure = error
            finally:
                if process is not None and process.poll() is None:
                    try:
                        os.killpg(process.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait()
        resource = (stage / "resource.txt").read_text() if (stage / "resource.txt").exists() else ""
        peak = re.search(r"^\s*(\d+)\s+maximum resident set size\s*$", resource, re.MULTILINE)
        record = dict(name=name, argv=list(map(str, argv)), cwd=str(cwd),
                      elapsed_seconds=time.monotonic() - started,
                      exit=process.returncode if process is not None else None, timed_out=timed_out,
                      darwin_max_process_rss_bytes=int(peak[1]) if peak else None,
                      sampled_tree_rss_bytes=maximum_tree, sampled_compiler_rss_bytes=maximum_compiler)
        self.results.append(record)
        write_json(stage / "result.json", record)
        write_json(self.output / "commands.json", self.results)
        print(json.dumps({"phase": name, "exit": record["exit"], "seconds": round(record["elapsed_seconds"], 2)}), flush=True)
        if failure:
            raise failure
        if record["exit"] != 0:
            raise RuntimeError("command failed; inspect " + str(stage))
        return (stage / "stdout.log").read_text()


def verify_inputs(candidate, manifest):
    expected = {1: {"full", "ordinary"}, 2: {"full", "ordinary", "configured"}}.get(manifest.get("format"))
    if expected is None or len(manifest["artifacts"]) != 3 or set(manifest["consumers"]) != expected:
        raise ValueError("unsupported or incomplete release manifest")
    for artifact in manifest["artifacts"]:
        path = candidate / artifact["zip"]
        if not under(path, candidate) or digest(path) != artifact["sha256"]:
            raise ValueError("candidate archive does not match manifest")
        for file in artifact["proxy_files"]:
            path = candidate / file["path"]
            if not under(path, candidate / "proxy") or path.is_symlink() or digest(path) != file["sha256"]:
                raise ValueError("candidate module metadata does not match manifest")
    for name, files in manifest["consumers"].items():
        root = candidate / "consumers" / name
        if not under(root, candidate / "consumers"):
            raise ValueError("invalid consumer path")
        actual = set()
        for path in root.rglob("*"):
            if path.is_symlink():
                raise ValueError("consumer source contains a symbolic link")
            if path.is_file():
                actual.add(str(path.relative_to(root)))
        if actual != {file["path"] for file in files}:
            raise ValueError("consumer file set does not match release manifest")
        for file in files:
            path = root / file["path"]
            if not under(path, root) or digest(path) != file["sha256"]:
                raise ValueError("consumer source does not match release manifest")


def generated_inventory(root):
    files = sorted(path for path in root.rglob("*_foundry.gen.*") if path.is_file())
    return {"files": len(files), "bytes": sum(path.stat().st_size for path in files),
            "sha256": {str(path.relative_to(root)): digest(path) for path in files}}


def position(source, prefix, symbol, operation):
    needle = prefix + symbol
    if source.count(needle) != 1:
        raise ValueError("measurement expression must occur exactly once")
    before = source[:source.index(needle) + len(prefix) + (operation != "complete")]
    return before.count("\n") + 1, len(before.rsplit("\n", 1)[-1].encode("utf-8")) + 1


def validate_editor(payload, operation, symbol):
    result = payload["result"]
    if operation == "complete":
        items = result["items"] if isinstance(result, dict) else result
        if not any(item["label"] == symbol or item["label"].startswith(symbol + "(") for item in items):
            raise ValueError("gopls response omitted expected generated method")
    elif symbol not in json.dumps(result):
        raise ValueError("gopls hover lost expected generated method")
    if payload["timing"]["request_ns"] <= 0 or payload["timing"]["initialize_ns"] <= 0:
        raise ValueError("editor timing is missing")


def profile(runner, name, consumer, go, foundry, gopls, base_env, rounds):
    report = {"source": name, "generated_before": generated_inventory(consumer), "editor": []}
    generation_env = dict(base_env, GOCACHE=str(runner.output / (name + "-generation-cache")))
    generate = [foundry, "generate", "--recursive", "--dir", consumer]
    runner.run(name + "-generation-cold", generate, consumer, generation_env)
    report["generated_after"] = generated_inventory(consumer)
    if report["generated_before"] != report["generated_after"]:
        raise ValueError("released generated output is not reproducible in independent consumer")
    runner.run(name + "-generation-unchanged", generate, consumer, generation_env)
    build_env = dict(base_env, GOCACHE=str(runner.output / (name + "-build-cache")))
    runner.run(name + "-build-cold", [go, "build", "./..."], consumer, build_env)
    runner.run(name + "-build-unchanged", [go, "build", "./..."], consumer, build_env)
    # One schema edit, confined to the cloned consumer, tests actual incremental
    # regeneration and dependent compilation rather than a timestamp-only touch.
    source = consumer / "productionprofile/record.go"
    original = source.read_text()
    marker = "\t// Measurement harness inserts one field here in its independent copy."
    if original.count(marker) != 1:
        raise ValueError("incremental fixture marker is missing or ambiguous")
    source.write_text(original.replace(marker, "\tRevision string\n" + marker))
    runner.run(name + "-generation-edited", generate, consumer, generation_env)
    report["generated_edited"] = generated_inventory(consumer)
    runner.run(name + "-build-edited", [go, "build", "./..."], consumer, build_env)
    # Use fresh servers for every request, matching the shipped agent command.
    # First request gets an empty gopls disk cache; later requests reuse it.
    editor_env = dict(build_env, GOPLSCACHE=str(runner.output / (name + "-gopls-cache")))
    file, prefix, symbol = {
        "ordinary": ("productionprofile/queries.go", "RecordDraft{}.", "SetName"),
        "configured": ("configuredprofile/routes.go", "services.", "Cache"),
        "full": ("catalog/product.go", "models.UserDraft{}.", "SetID"),
    }[name]
    text = (consumer / file).read_text()
    for round_number in range(rounds):
        for operation in ("complete", "hover"):
            line, column = position(text, prefix, symbol, operation)
            argv = [foundry, "agent", operation, "--workspace", consumer, "--file", file,
                    "--line", str(line), "--column", str(column), "--gopls", gopls, "--timeout", "120s"]
            result = json.loads(runner.run(f"{name}-gopls-{operation}-{round_number + 1}", argv, consumer, editor_env))
            validate_editor(result, operation, symbol)
            report["editor"].append({"operation": operation, "round": round_number + 1, "timing": result["timing"], "server": result["server"]})
    if (consumer / file).read_text() != text:
        raise ValueError("editor probe modified source")
    imports = runner.run(name + "-imports", [go, "list", "-deps", "./..."], consumer, build_env)
    report["imported_packages"] = len(set(imports.splitlines()))
    if name in ("ordinary", "configured"):
        directory = "productionprofile" if name == "ordinary" else "configuredprofile"
        executable = runner.output / (name + "-profile")
        runner.run(name + "-link", [go, "build", "-o", executable, "./" + directory + "/cmd/profile"], consumer, build_env)
        report["linked_binary_bytes"] = executable.stat().st_size
        report["linked_binary_sha256"] = digest(executable)
        runner.run(name + "-linked-smoke", [executable], consumer, build_env)
    if name == "configured":
        runner.run("configured-consumer-test", [go, "test", "-count=1", "-run", "^TestConfiguredProfile", "./configuredprofile"], consumer, build_env)
        runner.run("configured-runtime-benchmarks", [go, "test", "-run", "^$", "-bench", "Benchmark(Startup|Selection|HTTP)",
                   "-benchmem", "-benchtime=1s", "-count=3", "./configuredprofile"], consumer, build_env)
    return report


def module_review(modules, module_cache, output):
    records = []
    for module in modules:
        if module.get("Replace"):
            raise ValueError("release consumer graph still contains a replacement")
        if module.get("Main"):
            continue
        directory = Path(module["Dir"]) if module.get("Dir") else None
        licenses = []
        if directory:
            if not under(directory, module_cache):
                raise ValueError("dependency source escaped the isolated module cache")
            for file in sorted(directory.iterdir()):
                if file.is_file() and file.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "PATENTS")):
                    licenses.append({"file": file.name, "bytes": file.stat().st_size, "sha256": digest(file)})
        records.append({"path": module["Path"], "version": module.get("Version"), "go": module.get("GoVersion"),
                        "licenses": licenses, "review": "required"})
    write_json(output / "dependency-review.json", records)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("--go", required=True, type=Path)
    parser.add_argument("--gopls", required=True, type=Path)
    parser.add_argument("--govulncheck", type=Path, help="existing approved scanner; no installation is automatic")
    parser.add_argument("--rounds", type=int, default=3)
    parser.add_argument("--timeout", type=int, default=1800, help="seconds per command")
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("this profile requires native macOS; keep VM/Linux profiles separate")
    if not 1 <= args.rounds <= 10 or not 1 <= args.timeout <= 7200:
        parser.error("rounds must be 1–10 and timeout 1–7200 seconds")
    candidate = args.candidate.resolve()
    for tool in (args.go, args.gopls, args.govulncheck):
        if tool is not None and not (tool.is_absolute() and tool.is_file() and os.access(tool, os.X_OK)):
            parser.error("tool paths must be absolute existing executables")
    manifest = json.loads((candidate / "manifest.json").read_text())
    verify_inputs(candidate, manifest)
    output = candidate / "measurements"
    output.mkdir(mode=0o700)
    runner = Runner(output, args.timeout)
    module_cache = output / "module-cache"
    names = [item["path"] for item in manifest["artifacts"]]
    env = clean_environment(output / "setup-cache", module_cache, (candidate / "proxy").as_uri() + ",https://proxy.golang.org", names)
    env["PATH"] = str(args.go.parent) + os.pathsep + env.get("PATH", "")
    full = candidate / "consumers/full"
    report = {"status": "incomplete", "manifest_sha256": digest(candidate / "manifest.json"),
              "platform": platform.platform(), "machine": platform.machine(), "cpu_count": os.cpu_count(),
              "disk_before": dict(zip(("total", "used", "free"), shutil.disk_usage(output))),
              "go_requirement": manifest["go"], "profiles": [], "security": "not-run"}
    try:
        report["hardware"] = runner.run("hardware", ["/usr/sbin/sysctl", "hw.model", "hw.memsize", "hw.physicalcpu", "hw.logicalcpu"], full, env).strip()
        report["toolchain"] = json.loads(runner.run("toolchain", [args.go, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOROOT"], full, env))
        for name in sorted(manifest["consumers"]):
            consumer = candidate / "consumers" / name
            runner.run(name + "-download", [args.go, "mod", "download", "all"], consumer, env)
        modules = decode_stream(runner.run("module-graph", [args.go, "list", "-m", "-json", "all"], full, env))
        module_review(modules, module_cache, output)
        selected = {module["Path"]: module for module in modules}
        for artifact in manifest["artifacts"]:
            if selected[artifact["path"]].get("Version") != artifact["version"]:
                raise ValueError("module graph selected the wrong candidate version")
        runner.run("module-integrity", [args.go, "mod", "verify"], full, env)
        foundry = output / "foundry"
        runner.run("generator-build", [args.go, "build", "-o", foundry, names[0] + "/cmd/foundry"], full, env)
        for name in ("ordinary", "configured", "full"):
            if name not in manifest["consumers"]:
                continue
            report["profiles"].append(profile(runner, name, candidate / "consumers" / name, args.go, foundry, args.gopls, env, args.rounds))
            write_json(output / "report.json", report)
        if args.govulncheck:
            report["scanner"] = runner.run("vulnerability-tool", [args.govulncheck, "-version"], full, env).strip()
            findings = []
            for name, directory in (("framework", Path(selected[names[0]]["Dir"])), ("consumer", full)):
                raw = runner.run(name + "-vulnerabilities", [args.govulncheck, "-json", "./..."], directory, env)
                analysis = inspect_vulnerabilities(raw)
                findings.extend(analysis["findings"])
            write_json(output / "vulnerability-findings.json", findings)
            report["security"] = "review-required"
            # JSON mode can return zero even when it reports reachable findings.
            if any(frame.get("function") for finding in findings for frame in finding.get("trace", [])):
                raise ValueError("known vulnerable functions reported; review retained findings")
        report["status"] = "measured"
    finally:
        report["disk_after"] = dict(zip(("total", "used", "free"), shutil.disk_usage(output)))
        write_json(output / "report.json", report)


if __name__ == "__main__":
    os.umask(0o077)
    main()
