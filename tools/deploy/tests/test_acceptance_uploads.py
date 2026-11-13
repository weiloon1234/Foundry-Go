"""Pure controller checks. These never provision or contact a host."""
from pathlib import Path
import errno
import tempfile
import unittest
from unittest.mock import Mock, patch

import acceptance_uploads as acceptance
import uploads


class UploadAcceptanceTests(unittest.TestCase):
    def test_request_preserves_status_for_non_json_restart_response(self):
        for body, expected in ((b'<html>502 Bad Gateway</html>', None),
                               (b'\xff', None), (b'', None), (b'{"active": 0}', {'active': 0})):
            with self.subTest(body=body):
                connection = Mock()
                response = connection.getresponse.return_value
                response.status = 502
                response.read.return_value = body
                with patch.object(acceptance, 'connection', return_value=connection):
                    self.assertEqual((502, expected), acceptance.request({}, '/usage'))
                connection.close.assert_called_once()

    def test_fill_volume_exhausts_smaller_writes_and_partial_blocks(self):
        for code in (errno.ENOSPC, errno.EDQUOT):
            with self.subTest(code=code):
                remaining = len(acceptance.BLOCK) + 15 * 4096 + 3
                failures = []
                def write(data):
                    nonlocal remaining
                    if len(data) > remaining:
                        failures.append(len(data))
                        raise OSError(code, 'full')
                    # Raw writes may make partial progress before ENOSPC.
                    written = min(len(data), 4096)
                    remaining -= written
                    return written
                acceptance.fill_volume(Mock(write=write))
                self.assertEqual(0, remaining)
                self.assertEqual(len(acceptance.BLOCK), failures[0])
                self.assertEqual(1, failures[-1])

    def test_fill_volume_preserves_other_io_failures(self):
        with self.assertRaises(OSError) as raised:
            acceptance.fill_volume(Mock(write=Mock(side_effect=OSError(errno.EIO, 'failed'))))
        self.assertEqual(errno.EIO, raised.exception.errno)

    def test_full_scenarios_continue_with_less_than_one_free_block(self):
        for pool in ('proxy', 'request', 'attachment', 'staging'):
            with self.subTest(pool=pool), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                (root / 'data').mkdir()
                host = Mock()
                host.paths.data.return_value = root
                scenario = acceptance.Scenarios(host, {'name': 'fixture'}, {pool: root})
                scenario.small, scenario.clean, scenario.success = Mock(), Mock(), Mock()
                first = {'status': 200 if pool == 'proxy' else 507}
                recovered = {'status': 200}
                stats = Mock(f_bavail=15, f_frsize=4096)
                with patch.object(acceptance, 'fill_volume') as fill, \
                        patch.object(acceptance.os, 'statvfs', return_value=stats), \
                        patch.object(acceptance, 'upload', side_effect=[first, recovered]) as upload:
                    scenario.full(pool)
                fill.assert_called_once()
                self.assertEqual(2, upload.call_count)
                self.assertEqual(2, scenario.small.call_count)
                self.assertEqual(2, scenario.clean.call_count)
                self.assertEqual(2 if pool == 'proxy' else 1, scenario.success.call_count)
                self.assertEqual([], list((root / 'data').iterdir()))

    def test_profile_reserves_all_service_processes(self):
        value = acceptance.policy()
        uploads.validate(value)
        self.assertGreater(value['request_slots'], value['proxy_slots'])
        self.assertGreater(value['attachment_slots'], value['staging_slots'])
        minimum = uploads.minimum_bytes(value)
        for pool in ('attachment', 'staging'):
            self.assertGreater(value['pools'][pool], 3 * minimum[pool])
        self.assertGreater(value['proxy_read_idle_seconds'], 150)
        self.assertGreater(value['storage_total_seconds'], 150)

    def test_disk_sampler_counts_owned_files_and_nested_captures(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'capture').mkdir()
            (root / 'capture/file').write_bytes(b'1234')
            (root / 'marker').write_bytes(b'12')
            self.assertEqual(6, acceptance.occupancy(root))

    def test_monitor_refuses_full_proxy_spool(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = {name: Path(temporary) / name for name in uploads.POOL_NAMES}
            for path in paths.values():
                (path / 'data').mkdir(parents=True)
            (paths['proxy'] / 'data/body').write_bytes(b'x' * acceptance.MIB)
            monitor = acceptance.Monitor(paths)
            with self.assertRaisesRegex(AssertionError, 'Nginx spooled'):
                with monitor:
                    monitor.thread.join(timeout=0.05)

    def test_upload_streams_fixed_and_chunked_without_payload_sized_buffer(self):
        class Response:
            status = 200
            def read(self, limit):
                return b'{}'
            def getheader(self, name, default):
                return 'application/json'
        class Connection:
            def __init__(self):
                self.headers, self.bytes, self.maximum = {}, 0, 0
                self.closed = False
            def putrequest(self, *args): pass
            def putheader(self, name, value): self.headers[name] = value
            def endheaders(self): pass
            def send(self, data):
                self.bytes += len(data)
                self.maximum = max(self.maximum, len(data))
            def getresponse(self): return Response()
            def close(self): self.closed = True
        for chunked in (False, True):
            connection = Connection()
            with patch.object(acceptance, 'connection', return_value=connection):
                result = acceptance.upload({}, 'owned', size=2 * acceptance.MIB, chunked=chunked)
            self.assertEqual(200, result['status'])
            self.assertLessEqual(connection.maximum, len(acceptance.BLOCK) + 16)
            self.assertTrue(connection.closed)
            if chunked:
                self.assertEqual('chunked', connection.headers['Transfer-Encoding'])
            else:
                self.assertEqual(connection.bytes, int(connection.headers['Content-Length']))

    def test_scenarios_are_wired_into_guarded_runner(self):
        runner = Path(__file__).with_name('run_acceptance.sh').read_text()
        self.assertIn('phase uploads /verification/deploy/tests/acceptance_uploads.py', runner)
        with patch.object(acceptance, 'guard', side_effect=RuntimeError('guarded')), \
                patch.object(acceptance, 'fixture') as fixture:
            with self.assertRaisesRegex(RuntimeError, 'guarded'):
                with acceptance.configured_fixture(): pass
            fixture.assert_not_called()
