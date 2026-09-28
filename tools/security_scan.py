#!/usr/bin/env python3
"""Independent warm-cache vulnerability gate; never install tools or rebuild packages."""
import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time


def decode_stream(text):
    """Decode the sequence of JSON objects emitted by Go tools."""
    decoder, values, offset = json.JSONDecoder(), [], 0
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        value, offset = decoder.raw_decode(text, offset)
        values.append(value)
    return values


def inspect_vulnerabilities(text):
    messages = decode_stream(text)
    configs = [item["config"] for item in messages if "config" in item]
    if len(configs) != 1 or configs[0].get("scanner_name") != "govulncheck" or configs[0].get("scan_level") != "symbol" or not configs[0].get("db_last_modified"):
        raise ValueError("missing complete symbol-level scanner configuration")
    findings = [item["finding"] for item in messages if "finding" in item]
    for finding in findings:
        if not isinstance(finding.get("osv"), str) or not finding.get("trace") or not all(isinstance(frame, dict) and frame.get("module") for frame in finding["trace"]):
            raise ValueError("malformed vulnerability finding")
    functions = [item for item in findings if any(frame.get("function") for frame in item["trace"])]
    return {"config": configs[0], "findings": findings, "affected_function_findings": functions}


def inspect_npm(text):
    report = json.loads(text)
    counts = report.get("metadata", {}).get("vulnerabilities", {})
    if not all(type(counts.get(key)) is int and counts[key] >= 0 for key in ("info", "low", "moderate", "high", "critical", "total")):
        raise ValueError("missing npm vulnerability summary")
    if sum(counts[key] for key in ("info", "low", "moderate", "high", "critical")) != counts["total"]:
        raise ValueError("inconsistent npm vulnerability summary")
    return {"vulnerabilities": counts}


def run_command(argv, cwd, output, env, timeout=900, output_limit=64 << 20):
    """Own the process group, deadline and retained bounded-output records."""
    started = time.monotonic()
    failure = None
    process = None
    stdout_path, stderr_path = output.with_suffix(".stdout.log"), output.with_suffix(".stderr.log")
    with stdout_path.open("w") as stdout, stderr_path.open("w") as stderr:
        try:
            process = subprocess.Popen(argv, cwd=cwd, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
            while True:
                if time.monotonic() - started > timeout:
                    raise TimeoutError("security tool exceeded its deadline")
                if stdout_path.stat().st_size + stderr_path.stat().st_size > output_limit:
                    raise ValueError("security tool exceeded its output bound")
                code = process.poll()
                if code is not None:
                    break
                time.sleep(0.1)
        except (OSError, ValueError, TimeoutError) as error:
            failure = str(error)
        finally:
            if process is not None:
                if process.poll() is None:
                    try:
                        os.killpg(process.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                    except PermissionError:
                        # A fast-exiting Darwin child can lose its signalable
                        # group before wait has reaped it. Verify actual exit.
                        process.wait(timeout=2)
                    try:
                        process.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                process.wait()
    if stdout_path.stat().st_size + stderr_path.stat().st_size > output_limit:
        failure = "security tool exceeded its output bound"
    text = stdout_path.read_text() if stdout_path.stat().st_size <= output_limit else ""
    return {"command": argv, "exit": process.returncode if process else None,
            "seconds": round(time.monotonic() - started, 3), "execution_error": failure}, text


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--govulncheck", required=True)
    parser.add_argument("--gopls", required=True)
    parser.add_argument("--node", required=True)
    parser.add_argument("--npm", required=True)
    parser.add_argument("--module", action="append", required=True)
    parser.add_argument("--output", type=Path, default=Path(".cache/security-check"))
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    go = shutil.which(args.go)
    if not go:
        parser.error("an existing Go executable is required")
    env["PATH"] = str(Path(go).parent) + os.pathsep + env.get("PATH", "")
    env["GOWORK"] = "off"
    for name in ("govulncheck", "gopls", "node", "npm"):
        selected = getattr(args, name)
        executable = shutil.which(selected)
        if not executable:
            parser.error("existing " + name + " executable is required")
        setattr(args, name, str(Path(executable).resolve()))
    scans = []
    for module in args.module:
        directory = (root / module).resolve()
        if not directory.is_relative_to(root) or not (directory / "go.mod").is_file():
            parser.error("scan modules must be repository module roots")
        scans.append(("go-" + str(len(scans)), [args.govulncheck, "-json", "./..."], directory, inspect_vulnerabilities))
    scans.extend([
        ("gopls", [args.govulncheck, "-mode=binary", "-json", args.gopls], root, inspect_vulnerabilities),
        ("typescript", [args.node, args.npm, "audit", "--package-lock-only", "--ignore-scripts", "--json"], root / "tools/typescript", inspect_npm),
    ])
    report = {"format": 1, "status": "running", "checks": [], "policy": "Fail tool errors, malformed/incomplete results, affected function findings or any npm vulnerability. Preserve module/package-only findings for review."}
    failed = False
    for name, command, directory, inspect in scans:
        result, text = run_command(command, directory, output / name, env)
        result["name"], result["module"] = name, str(directory.relative_to(root))
        try:
            result["analysis"] = inspect(text)
        except (ValueError, KeyError, TypeError, AttributeError) as error:
            result["analysis_error"] = str(error)
        analysis = result.get("analysis", {})
        bad = bool(result["execution_error"] or result["exit"] != 0 or result.get("analysis_error") or analysis.get("affected_function_findings") or analysis.get("vulnerabilities", {}).get("total", 0))
        failed = failed or bad
        result["status"] = "failed" if bad else "passed"
        report["checks"].append(result)
        (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(name + ": " + result["status"], flush=True)
    report["status"] = "failed" if failed else "passed"
    (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    return 1 if failed else 0


if __name__ == "__main__":
    os.umask(0o077)
    raise SystemExit(main())
