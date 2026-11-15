"""Shared disposable systemd container setup for rehearsal and acceptance."""
import argparse
from contextlib import contextmanager
import errno
import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import uuid

S3_IMAGE = "versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316"
# Update only with the retained acceptance result described in the provider evidence.
# A pending/failed matrix never grants multipart conditions to rehearsal.
S3_CONDITIONAL_MULTIPART = {
    "image": "versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316",
    "status": "failed",
    "evidence": "docs/evidence/v036-rehearsal-s3-certification-20261115.json",
    "gap": "ListMultipartUploads omits EncodingType=url; conditional CompleteMultipartUpload remains uncertified",
}
S3_CONDITIONAL_TEST = "TestRealCompatibleConditionalMultipart"
LABEL = "org.foundry.rehearsal"
ASSETS = Path(__file__).resolve().parent


class ContainerError(RuntimeError):
    pass


def s3_certification():
    record = S3_CONDITIONAL_MULTIPART
    if (record["image"] != S3_IMAGE or record["status"] not in ("pending", "passed", "failed")
            or record["status"] != "pending" and not record["evidence"]):
        raise ContainerError("S3 image changed or lacks evidence; re-certify conditional multipart before rehearsal")
    return dict(record)


def run(arguments, *, input=None, timeout=120, check=True, env=None):
    # Docker and build output can contain private application values. Bound output
    # without retaining it in memory or exposing it through command exceptions.
    with tempfile.TemporaryFile() as output:
        process = subprocess.Popen([str(a) for a in arguments], stdin=subprocess.PIPE,
            stdout=output, stderr=subprocess.DEVNULL, start_new_session=True, env=env)
        try:
            process.communicate(input.encode() if input is not None else None, timeout=timeout)
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
            raise ContainerError("Container command interrupted; output withheld") from None
        if output.tell() > 4 * 1024 * 1024:
            raise ContainerError("Container command output exceeded its bound")
        output.seek(0)
        result = subprocess.CompletedProcess(arguments, process.returncode, output.read().decode(), "")
    if check and result.returncode:
        raise ContainerError("Container command failed; output withheld")
    return result


def create_host(name, image, network, platform, mounts, *, cpus=2, memory_gib=4, extra=()):
    arguments = ["docker", "create", "--platform", platform, "--name", name, "--network", network,
        "--privileged", "--cgroupns=private", f"--memory={memory_gib}g", f"--cpus={cpus}",
        "--tmpfs", "/run", "--tmpfs", "/run/lock", "--tmpfs", "/tmp", *extra]
    for mount in mounts:
        arguments += ["--mount", mount]
    return run([*arguments, image]).stdout.strip()


def create_ca(directory):
    directory = Path(directory)
    directory.mkdir(mode=0o700, exist_ok=True)
    (directory / "ca.cnf").write_text("""[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no
[dn]
CN = Foundry-acceptance-CA
[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always
""")
    run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2", "-sha256",
         "-config", directory / "ca.cnf", "-keyout", directory / "ca.key", "-out", directory / "ca.crt"])
    (directory / "ca.key").chmod(0o600)
    run(["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN=s3",
         "-keyout", directory / "private.key", "-out", directory / "s3.csr"])
    (directory / "s3.ext").write_text("""basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:s3
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid,issuer
""")
    run(["openssl", "x509", "-req", "-in", directory / "s3.csr", "-CA", directory / "ca.crt",
         "-CAkey", directory / "ca.key", "-CAcreateserial", "-days", "2", "-sha256",
         "-extfile", directory / "s3.ext", "-out", directory / "public.crt"])
    (directory / "private.key").chmod(0o600)


def start_host(name):
    run(["docker", "start", name])
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if run(["docker", "exec", name, "test", "-S", "/run/systemd/private"], check=False).returncode == 0:
            break
        time.sleep(1)
    else:
        raise ContainerError("Disposable systemd bus did not start")
    run(["docker", "exec", name, "systemctl", "start", "multi-user.target"])


def boot_host(name, certs):
    start_host(name)
    run(["docker", "exec", name, "mkdir", "-m", "700", "-p", "/etc/foundry/acceptance-ca"])
    for file in ("ca.crt", "ca.key"):
        run(["docker", "cp", Path(certs) / file, name + ":/etc/foundry/acceptance-ca/" + file])
    run(["docker", "exec", name, "chown", "-R", "root:root", "/etc/foundry/acceptance-ca"])
    run(["docker", "exec", name, "chmod", "600", "/etc/foundry/acceptance-ca/ca.key", "/etc/foundry/acceptance-ca/ca.crt"])
    run(["docker", "exec", name, "cp", "/etc/foundry/acceptance-ca/ca.crt", "/usr/local/share/ca-certificates/foundry-acceptance.crt"])
    run(["docker", "exec", name, "chmod", "644", "/usr/local/share/ca-certificates/foundry-acceptance.crt"])
    run(["docker", "exec", name, "update-ca-certificates"])


class Resources:
    """Record names before creation; inspect ownership before every removal."""
    def __init__(self, directory):
        self.directory = Path(directory)
        self.token = uuid.uuid4().hex
        self.items = []

    def reserve(self, kind, role):
        name = f"foundry-rehearse-{role}-{self.token}"
        self.items.append((kind, name))
        (self.directory / "resources.json").write_text(json.dumps({"run_id": self.token, "resources": self.items}, indent=2) + "\n")
        return name

    @property
    def label(self):
        return LABEL + "=" + self.token

    def cleanup(self):
        failed = []
        for kind, name in reversed(self.items):
            try:
                result = run(["docker", kind, "inspect", name], check=False)
                if result.returncode:
                    # A failed inspect is not proof of absence. List through the
                    # daemon, including stopped containers, and match the exact name.
                    listing = run(["docker", kind, "ls", *(["--all"] if kind == "container" else []),
                                   "--format", "{{json .}}"], check=False)
                    rows = [json.loads(line) for line in listing.stdout.splitlines()]
                    key = "Names" if kind == "container" else "Name"
                    present = (any(row.get("Repository") + ":" + row.get("Tag") == name + ":latest" for row in rows)
                               if kind == "image" else any(row.get(key) == name for row in rows))
                    if listing.returncode or present:
                        failed.append(name)
                    continue
                value, = json.loads(result.stdout)
                labels = value.get("Config", {}).get("Labels") if kind in ("container", "image") else value.get("Labels")
                if not labels or labels.get(LABEL) != self.token:
                    failed.append(name)
                    continue
                command = ["docker", kind, "rm"] + (["-f", "-v"] if kind == "container" else [])
                if run([*command, value.get("Id", name)], check=False).returncode:
                    failed.append(name)
            except (OSError, ContainerError, ValueError, TypeError, KeyError):
                failed.append(name)
        return failed


def _upload_root_directory(path):
    from contracts import check_path, require
    check_path(path, "directory")
    for parent in (path, *path.parents):
        info = parent.stat()
        require(info.st_uid == 0 and not info.st_mode & 0o022,
                "upload_standin_ownership", "Stand-in parents must be root-controlled.")


@contextmanager
def _upload_volume_lock(workdir):
    from contracts import check_path, require
    require(os.geteuid() == 0, "upload_standin_ownership", "Stand-in volumes require root.")
    _upload_root_directory(workdir.parent)
    # The lock outlives the journal. A disconnected docker exec may still be
    # provisioning when another exec enters cleanup.
    lock = check_path(workdir.parent / ".upload-volumes.lock", "file")
    with open(lock, "a", opener=lambda path, flags: os.open(path, flags, 0o600)) as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        yield


def provision_upload_volumes(paths, quotas, workdir):
    """Journal ownership before mutation; undo every attempted mount on failure."""
    from contracts import atomic_write, check_path, require
    workdir = Path(workdir)
    with _upload_volume_lock(workdir):
        require(not workdir.exists(), "upload_standin_exists", "Stand-in work directory already exists.")
        directories, images = [], []
        for name, path in paths.items():
            path = check_path(Path(path), "directory")
            require(not path.exists(), "upload_standin_exists", "Refusing an existing upload volume.")
            require(name in quotas and type(quotas[name]) is int and quotas[name] > 0
                    and Path(name).name == name and name not in (".", ".."),
                    "upload_standin_plan", "Invalid stand-in pool or quota.")
            require(not workdir.is_relative_to(path) and not path.is_relative_to(workdir),
                    "upload_standin_plan", "Stand-in images must be outside the upload pools.")
            missing, cursor = [], path
            while not cursor.exists():
                missing.append(cursor)
                cursor = cursor.parent
            _upload_root_directory(cursor)
            for directory in reversed(missing):
                if str(directory) not in directories:
                    directories.append(str(directory))
            images.append(str(workdir / (name + ".ext4")))
        state = {"paths": [str(p) for p in paths.values()], "images": images, "directories": directories}
        try:
            workdir.mkdir(mode=0o700)
            workdir.chmod(0o700)
            atomic_write(workdir / "volumes.json", json.dumps(state), mode=0o600)
            for directory in directories:
                Path(directory).mkdir(mode=0o755)
                Path(directory).chmod(0o755)
            for (name, path), image in zip(paths.items(), images):
                run(["truncate", "-s", str(quotas[name]), image])
                run(["mkfs.ext4", "-q", "-m", "0", "-F", image])
                run(["mount", "-o", "loop,nodev,nosuid", image, str(path)])
        except BaseException:
            _release_upload_volumes(workdir)
            raise


def _release_upload_volumes(workdir):
    from contracts import check_path, read_json, require
    if not workdir.exists():
        return
    journal = check_path(workdir / "volumes.json", "file")
    if journal.exists():
        state = read_json(journal)
        failed = []
        for path in reversed(state["paths"]):
            try:
                if os.path.ismount(path):
                    run(["umount", path])
            except Exception:
                failed.append(path)
        require(not failed, "upload_standin_cleanup", "Upload mounts remain; retain the disposable host and retry cleanup.")
        for image in state["images"]:
            Path(image).unlink(missing_ok=True)
        for directory in reversed(state["directories"]):
            try:
                Path(directory).rmdir()
            except FileNotFoundError:
                pass
            except OSError as error:
                # Bootstrap can subsequently populate a newly created shared
                # parent such as /var/lib/foundry. Remove only empty directories.
                if error.errno not in (errno.ENOTEMPTY, errno.EEXIST):
                    raise
        journal.unlink()
    workdir.rmdir()


def release_upload_volumes(workdir):
    """Unmount before deleting images; keep the journal if cleanup needs a retry."""
    workdir = Path(workdir)
    with _upload_volume_lock(workdir):
        _release_upload_volumes(workdir)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("s3-image")
    ca = commands.add_parser("ca")
    ca.add_argument("directory")
    start = commands.add_parser("start-host", help="Start systemd without installing Foundry resources")
    start.add_argument("name")
    boot = commands.add_parser("boot")
    boot.add_argument("name")
    boot.add_argument("certs")
    create = commands.add_parser("create-host")
    for name in ("name", "image", "network", "platform"):
        create.add_argument("--" + name, required=True)
    create.add_argument("--mount", action="append", default=[])
    args = parser.parse_args()
    if args.command == "s3-image":
        print(S3_IMAGE)
    elif args.command == "ca":
        create_ca(args.directory)
    elif args.command == "start-host":
        start_host(args.name)
    elif args.command == "boot":
        boot_host(args.name, args.certs)
    else:
        print(create_host(args.name, args.image, args.network, args.platform, args.mount))


if __name__ == "__main__":
    main()
