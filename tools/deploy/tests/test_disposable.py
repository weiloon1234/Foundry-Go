"""Disposable VM opt-in must never turn an environment variable into authority."""
from contextlib import contextmanager
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import disposable
import acceptance_support


@contextmanager
def marker_owner(marker, uid):
    original = Path.lstat

    def lstat(path, *args, **kwargs):
        result = original(path, *args, **kwargs)
        if path == marker:
            fields = list(result)
            fields[4] = uid
            return os.stat_result(fields)
        return result

    with patch.object(Path, "lstat", lstat):
        yield


class DisposableHostTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.marker = self.root / disposable.VM_MARKER.lstrip("/")
        self.marker.parent.mkdir()
        self.environment = patch.dict(os.environ, {"FOUNDRY_DISPOSABLE_HOST": "1"})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def test_environment_without_marker_is_refused(self):
        self.assertFalse(disposable.disposable_host(self.root))
        with patch.object(acceptance_support, "disposable_host", lambda: disposable.disposable_host(self.root)), \
                patch.object(acceptance_support.os, "geteuid", return_value=0):
            with self.assertRaisesRegex(RuntimeError, "disposable"):
                acceptance_support.guard()

    def test_vm_requires_environment_and_root_owned_private_regular_file(self):
        self.marker.touch(mode=0o600)
        with marker_owner(self.marker, 0):
            self.assertTrue(disposable.disposable_host(self.root))
            with patch.dict(os.environ, {"FOUNDRY_DISPOSABLE_HOST": "0"}):
                self.assertFalse(disposable.disposable_host(self.root))
            for mode in (0o644, 0o620, 0o666):
                self.marker.chmod(mode)
                self.assertFalse(disposable.disposable_host(self.root), oct(mode))
            self.marker.chmod(0o600)
        with marker_owner(self.marker, 1000):
            self.assertFalse(disposable.disposable_host(self.root))

    def test_symlink_directory_and_fifo_markers_are_refused(self):
        target = self.root / "target"
        target.touch(mode=0o600)
        self.marker.symlink_to(target)
        with marker_owner(self.marker, 0):
            self.assertFalse(disposable.disposable_host(self.root))
        self.marker.unlink()
        self.marker.mkdir(mode=0o600)
        with marker_owner(self.marker, 0):
            self.assertFalse(disposable.disposable_host(self.root))
        self.marker.rmdir()
        os.mkfifo(self.marker, mode=0o600)
        with marker_owner(self.marker, 0):
            self.assertFalse(disposable.disposable_host(self.root))

    def test_shared_guard_accepts_only_root_with_opt_in(self):
        self.marker.touch(mode=0o600)
        with marker_owner(self.marker, 0), \
                patch.object(acceptance_support, "disposable_host", lambda: disposable.disposable_host(self.root)), \
                patch.object(acceptance_support, "WORK", self.root / "work"), \
                patch.object(acceptance_support.os, "umask"), \
                patch.object(acceptance_support.os, "geteuid", return_value=0):
            acceptance_support.guard()
            self.assertTrue((self.root / "work").is_dir())
            with patch.object(acceptance_support.os, "geteuid", return_value=1000):
                with self.assertRaises(RuntimeError):
                    acceptance_support.guard()
            with patch.dict(os.environ, {"FOUNDRY_DISPOSABLE_HOST": "0"}):
                with self.assertRaises(RuntimeError):
                    acceptance_support.guard()

    def test_existing_container_marker_remains_supported(self):
        (self.root / ".dockerenv").touch()
        with patch.dict(os.environ, {"FOUNDRY_DISPOSABLE_HOST": "0"}):
            self.assertTrue(disposable.disposable_host(self.root))
