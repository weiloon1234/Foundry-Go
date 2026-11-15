#!/usr/bin/env python3
"""Certify the pinned S3 stand-in on acceptance's existing disposable host."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "local"))
from containers import ContainerError, S3_IMAGE, S3_CONDITIONAL_TEST, run


def matrix_passed(exit_code, cases):
    passed = {case["test"] for case in cases if case["status"] == "pass"}
    required = {S3_CONDITIONAL_TEST, *(S3_CONDITIONAL_TEST + "/" + size for size in ("5242880", "5242881"))}
    return exit_code == 0 and required <= passed and all(case["status"] == "pass" for case in cases)


def matrix_result(result):
    cases, diagnostics, active = [], {}, None
    test_name = S3_CONDITIONAL_TEST + r"[\w/-]*"
    for line in result.stdout.splitlines():
        context = re.fullmatch(r"=== (?:RUN|NAME)\s+(" + test_name + r")\s*", line)
        if context:
            active = context[1]
            continue
        finished = re.match(r"\s*--- (PASS|FAIL|SKIP): (" + test_name + r") \(", line)
        if finished:
            status, name = finished.groups()
            cases.append({"test": name, "status": status.lower()})
            continue
        failure = re.fullmatch(r"\s*((?:provider|conditional_multipart)_test\.go:[0-9]{1,6}): (.*)", line)
        if active and failure:
            diagnostics.setdefault(active, safe_failure(failure[1], failure[2]))
    # Go can report parent results before children. Attribute diagnostics only
    # after all statuses are known, keeping their original observation order.
    failed = {case["test"] for case in cases if case["status"] == "fail"}
    failures = {}
    for name, message in diagnostics.items():
        if name in failed:
            while name:
                failures.setdefault(name, message)
                name = name.rpartition("/")[0]
    for case in cases:
        if case["status"] == "fail":
            case["first_failure"] = failures.get(case["test"], "Test failed; diagnostic unavailable")
    ok = matrix_passed(result.returncode, cases)
    return {"status": "passed" if ok else "failed", "exit_code": result.returncode, "cases": cases}


def safe_failure(location, diagnostic):
    # Allowlist summaries and framework codes instead of trying to redact arbitrary
    # SDK text, which can contain endpoints, signed URLs, headers and object keys.
    summary = ("multipart inspection precondition failed"
               if diagnostic.startswith("certification needs multipart inspection permissions")
               else "test assertion failed (diagnostic withheld)")
    fault = re.search(r"\bstorage_(?:not_found|forbidden|unsupported|precondition_failed|range_not_satisfiable|"
                      r"limit_exceeded|invalid|unavailable|integrity_failed|closed): "
                      r"(?:put|open|stat|delete|list|copy|move|sign|close)\b", diagnostic)
    return (location + ": " + summary + ("; " + fault[0] if fault else ""))[:256]


def in_host(binary):
    from acceptance_backup import rclone, s3_settings
    from acceptance_support import guard
    guard()
    settings = s3_settings()
    bucket = "certification-" + uuid.uuid4().hex
    # Only this disposable acceptance phase provisions a fresh bucket. The Go
    # provider matrix still owns and cleans its random object/upload descendants.
    rclone(settings, "mkdir", "acceptance:" + bucket)
    environment = {**os.environ, "FOUNDRY_TEST_COMPATIBLE_REQUIRED": "1",
        "FOUNDRY_TEST_COMPATIBLE_BUCKET": bucket, "FOUNDRY_TEST_COMPATIBLE_PREFIX": "conditional/",
        "FOUNDRY_TEST_COMPATIBLE_ENDPOINT": settings["endpoint"], "FOUNDRY_TEST_COMPATIBLE_REGION": "us-east-1",
        "FOUNDRY_TEST_COMPATIBLE_PUBLIC_BASE": "", "FOUNDRY_TEST_COMPATIBLE_PROFILE": "",
        "FOUNDRY_TEST_COMPATIBLE_SESSION_TOKEN": "",
        "FOUNDRY_TEST_COMPATIBLE_ACCESS_KEY_ID": settings["access_key"],
        "FOUNDRY_TEST_COMPATIBLE_SECRET_ACCESS_KEY": settings["secret_key"]}
    result = run([binary, "-test.v", "-test.timeout=25m", "-test.count=1",
                  "-test.run=^" + S3_CONDITIONAL_TEST + "$"], env=environment, timeout=1560, check=False)
    evidence = matrix_result(result)
    if evidence["status"] == "passed":
        rclone(settings, "rmdir", "acceptance:" + bucket)
    print(json.dumps(evidence))
    return 0 if evidence["status"] == "passed" else 1


def certify(host, store, platform, env_file, output):
    root = Path(__file__).resolve().parents[3]
    output = Path(output)
    # A rerun gets a new evidence file, never overwrites an earlier observation.
    with output.open("x", encoding="utf-8") as stream:
        output.chmod(0o600)
        evidence = {"image": S3_IMAGE, "platform": platform, "matrix": S3_CONDITIONAL_TEST, "status": "failed"}
        try:
            image = run(["docker", "inspect", "--format", "{{.Config.Image}}", store]).stdout.strip()
            if image != S3_IMAGE:
                raise ContainerError("Certification requires the pinned S3 image")
            evidence["image_id"] = run(["docker", "inspect", "--format", "{{.Image}}", store]).stdout.strip()
            with tempfile.TemporaryDirectory(prefix="foundry-s3-certification-") as temporary:
                binary = Path(temporary) / "s3.test"
                environment = {**os.environ, "GOOS": "linux", "GOARCH": platform.split("/")[1], "CGO_ENABLED": "0"}
                run([os.environ.get("GO", "go"), "-C", root, "test", "-c", "-o", binary, "./storage/s3"],
                    env=environment, timeout=1800)
                evidence["test_binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
                target = "/var/lib/foundry-acceptance-f4a/s3-certification.test"
                run(["docker", "cp", binary, host + ":" + target])
            result = run(["docker", "exec", "--env-file", env_file, host, "python3", "-B",
                          "/verification/deploy/tests/certify_s3.py", "--in-host", target], timeout=1620, check=False)
            observed = json.loads(result.stdout)
            evidence.update(observed)
            if result.returncode or evidence["status"] != "passed":
                evidence["status"] = "failed"
                raise ContainerError("Stand-in conditional multipart certification failed; see s3-certification.json")
        finally:
            json.dump(evidence, stream, indent=2)
            stream.write("\n")
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--in-host", metavar="BINARY")
    parser.add_argument("--host")
    parser.add_argument("--store")
    parser.add_argument("--platform", choices=("linux/arm64", "linux/amd64"))
    parser.add_argument("--env-file", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if args.in_host:
        return in_host(args.in_host)
    if not all((args.host, args.store, args.platform, args.env_file, args.output)):
        parser.error("Supply --host, --store, --platform, --env-file and --output")
    certify(args.host, args.store, args.platform, args.env_file, args.output)
    print("PASS pinned S3 conditional multipart certification")
    return 0


if __name__ == "__main__":
    sys.exit(main())
