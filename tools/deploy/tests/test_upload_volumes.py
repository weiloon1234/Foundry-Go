"""Disposable volume ownership and teardown without root, mounts or Docker."""
import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "local"))
from contracts import DeployError
import containers
import uploads
from test_uploads import profile


class UploadVolumeTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.work = self.root / "images"
        self.paths = {name: self.root / "pools" / "instance" / name for name in uploads.POOL_NAMES}
        self.quotas = profile()["pools"]
        self.mounted, self.commands = set(), []
        for target, kwargs in (("os.geteuid", {"return_value": 0}),
                               ("os.fchown", {}),
                               ("_upload_root_directory", {}),
                               ("os.path.ismount", {"side_effect": lambda p: str(p) in self.mounted}),
                               ("run", {"side_effect": self.command})):
            mock = patch("containers." + target, **kwargs)
            mock.start()
            self.addCleanup(mock.stop)

    def command(self, arguments):
        self.commands.append(arguments)
        if arguments[0] == "truncate":
            Path(arguments[-1]).touch()
        elif arguments[0] == "mount":
            self.mounted.add(arguments[-1])
        elif arguments[0] == "umount":
            # No image is deleted until every filesystem is unmounted.
            self.assertTrue(all((self.work / (name + ".ext4")).exists() for name in self.created_images))
            self.mounted.remove(arguments[-1])
        return subprocess.CompletedProcess(arguments, 0, "", "")

    @property
    def created_images(self):
        return [Path(command[-1]).stem for command in self.commands if command[0] == "truncate"]

    def provision(self):
        containers.provision_upload_volumes(self.paths, self.quotas, self.work)

    def test_declared_sizes_paths_modes_and_idempotent_release(self):
        old = os.umask(0o077)
        try:
            self.provision()
        finally:
            os.umask(old)
        self.assertEqual(self.work.stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.root / ".upload-volumes.lock").stat().st_mode & 0o777, 0o600)
        for path in [self.root / "pools", self.root / "pools/instance", *self.paths.values()]:
            self.assertEqual(path.stat().st_mode & 0o777, 0o755)
        expected = []
        for name, path in self.paths.items():
            image = str(self.work / (name + ".ext4"))
            expected.extend([["truncate", "-s", str(self.quotas[name]), image],
                             ["mkfs.ext4", "-q", "-m", "0", "-F", image],
                             ["mount", "-o", "loop,nodev,nosuid", image, str(path)]])
        self.assertEqual(self.commands, expected)
        containers.release_upload_volumes(self.work)
        self.assertEqual(self.commands[len(expected):], [["umount", str(p)] for p in reversed(self.paths.values())])
        self.assertFalse(self.work.exists())
        self.assertFalse((self.root / "pools").exists())
        before = list(self.commands)
        containers.release_upload_volumes(self.work)
        self.assertEqual(self.commands, before)

    def test_partial_mount_failure_rolls_back_in_reverse_order(self):
        def fail(arguments):
            result = self.command(arguments)
            if arguments[0] == "mount" and arguments[-1] == str(self.paths["request"]):
                raise RuntimeError("mount completion lost")
            return result
        with patch.object(containers, "run", side_effect=fail), self.assertRaisesRegex(RuntimeError, "completion lost"):
            self.provision()
        self.assertEqual([c for c in self.commands if c[0] == "umount"],
                         [["umount", str(self.paths[name])] for name in ("request", "proxy")])
        self.assertFalse(self.mounted)
        self.assertFalse(self.work.exists())
        self.assertFalse((self.root / "pools").exists())

    def test_failed_unmount_attempts_every_pool_preserves_images_and_retries(self):
        self.provision()
        def fail(arguments):
            if arguments == ["umount", str(self.paths["staging"])]:
                self.commands.append(arguments)
                raise RuntimeError("busy")
            return self.command(arguments)
        with patch.object(containers, "run", side_effect=fail), self.assertRaises(DeployError):
            containers.release_upload_volumes(self.work)
        self.assertEqual(len([c for c in self.commands if c[0] == "umount"]), len(self.paths))
        self.assertEqual(self.mounted, {str(self.paths["staging"])})
        self.assertTrue((self.work / "volumes.json").exists())
        self.assertEqual(len(list(self.work.glob("*.ext4"))), len(self.paths))
        containers.release_upload_volumes(self.work)
        self.assertFalse(self.work.exists())

    def test_mkfs_failure_removes_image_without_unmounting_unmounted_pool(self):
        def fail(arguments):
            result = self.command(arguments)
            if arguments[0] == "mkfs.ext4":
                raise RuntimeError("format failed")
            return result
        with patch.object(containers, "run", side_effect=fail), self.assertRaises(RuntimeError):
            self.provision()
        self.assertFalse(any(c[0] == "umount" for c in self.commands))
        self.assertFalse(self.work.exists())

    def test_existing_parents_are_not_chmodded_or_removed(self):
        parent = self.root / "pools"
        parent.mkdir(mode=0o700)
        self.provision()
        self.assertEqual(parent.stat().st_mode & 0o777, 0o700)
        containers.release_upload_volumes(self.work)
        self.assertTrue(parent.is_dir())

    def test_parent_populated_by_bootstrap_is_preserved(self):
        self.provision()
        other = self.root / "pools" / "bootstrap-owned"
        other.write_text("retain")
        containers.release_upload_volumes(self.work)
        self.assertEqual(other.read_text(), "retain")
        self.assertFalse(self.work.exists())
        self.assertFalse((self.root / "pools/instance").exists())

    def test_cleanup_waits_for_inflight_provisioning(self):
        mounting, finish, releasing = threading.Event(), threading.Event(), threading.Event()
        def command(arguments):
            if arguments[0] == "mount" and not mounting.is_set():
                mounting.set()
                self.assertTrue(finish.wait(5))
            return self.command(arguments)
        def release():
            releasing.set()
            containers.release_upload_volumes(self.work)
        with patch.object(containers, "run", side_effect=command), ThreadPoolExecutor(max_workers=2) as executor:
            provision = executor.submit(self.provision)
            try:
                self.assertTrue(mounting.wait(5))
                cleanup = executor.submit(release)
                self.assertTrue(releasing.wait(5))
                self.assertFalse(cleanup.done())
            finally:
                finish.set()
            provision.result(timeout=5)
            cleanup.result(timeout=5)
        self.assertFalse(self.mounted)
        self.assertFalse(self.work.exists())

    def test_existing_mount_path_and_images_inside_pool_are_refused_before_commands(self):
        self.paths["proxy"].mkdir(parents=True)
        with self.assertRaises(DeployError):
            self.provision()
        self.paths["proxy"].rmdir()
        self.paths = {"proxy": self.work / "proxy"}
        with self.assertRaises(DeployError):
            self.provision()
        self.assertEqual(self.commands, [])


class UploadVolumeOwnershipTests(unittest.TestCase):
    def test_existing_parents_must_be_root_controlled(self):
        for uid, mode in ((1, 0o755), (0, 0o775), (0, 0o777)):
            path = SimpleNamespace(parents=(), stat=lambda: SimpleNamespace(st_uid=uid, st_mode=mode))
            with patch("contracts.check_path"), self.assertRaises(DeployError):
                containers._upload_root_directory(path)


if __name__ == "__main__":
    unittest.main()
