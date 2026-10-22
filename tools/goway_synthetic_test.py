import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('goway_synthetic', Path(__file__).with_name('goway_synthetic.py'))
synthetic = importlib.util.module_from_spec(spec)
spec.loader.exec_module(synthetic)


class SyntheticTests(unittest.TestCase):
    def test_seed_and_shared_workload(self):
        a = synthetic.schema(5, 19)
        self.assertEqual(a, synthetic.schema(5, 19))
        self.assertNotEqual(a, synthetic.schema(5, 20))
        self.assertEqual(len(a['handlers']), 40)
        self.assertEqual(len(a['reports']), 5)
        for selected in a['handlers']:
            self.assertTrue(2 <= len(selected) <= 5)
            self.assertEqual(len(selected), len(set(selected)))
        for style in ('old', 'new'):
            files = synthetic.sources(a, style)
            self.assertEqual(len(files), 52)
            for i in range(5):
                source = files[f'models/entity{i:03d}.go']
                for name, typ, _, _ in synthetic.fields(i, 5):
                    self.assertIn(f'{name} {typ}', source)
                self.assertEqual('//foundry:model' in source, style == 'old')
                self.assertEqual('orm.Define(' in source, style == 'new')
            for i in range(40):
                source = files[f'handlers/handler{i:03d}/handler.go']
                for call in ('.Where(', '.OrderBy(', '.Limit(', '.All(', '.Find(', '.Count(', '.Update(', '.With('):
                    self.assertIn(call, source)
            self.assertIn('handler039.Run', files['app/app.go'])
            self.assertIn('report004.Run', files['app/app.go'])
            for i in range(5):
                self.assertIn('.InnerJoin(', files[f'reports/report{i:03d}/report.go'])
                self.assertIn('.All(ctx,db)', files[f'reports/report{i:03d}/report.go'])
        self.assertIn('CreatedAt', synthetic.model_source(0, 5, 'new'))
        self.assertNotIn('CreatedAt', synthetic.model_source(1, 5, 'new'))
        self.assertIn('SoftDeleteOf', synthetic.model_source(3, 5, 'new'))
        self.assertIn('HasManyOf', synthetic.model_source(0, 5, 'new'))
        self.assertIn('BelongsToOf', synthetic.model_source(4, 5, 'new'))
        with self.assertRaises(ValueError):
            synthetic.schema(1, 1)

    def test_target_rejects_repository_ancestors_and_symlinks(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            repo = root / 'repo'
            repo.mkdir()
            link = root / 'link'
            link.symlink_to(repo, target_is_directory=True)
            for path in (repo, repo / 'out', root, link / 'out'):
                with self.assertRaises(ValueError):
                    synthetic.outside(path, repo)
            self.assertEqual(synthetic.outside(root / 'output', repo), root / 'output')
            dangling = root / 'dangling'
            dangling.symlink_to(root / 'missing', target_is_directory=True)
            with self.assertRaises(ValueError):
                synthetic.generate(dangling, repo, synthetic.schema(2, 1))
            with patch.object(synthetic.platform, 'system', return_value='Darwin'), self.assertRaises(ValueError):
                synthetic.measure(root / 'output', dangling)


    def test_generate_uses_root_toolchain_and_retained_enum_generation(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            repo, target = root / 'repo', root / 'app'
            fixture = repo / 'tests/fixtures/goway'
            fixture.mkdir(parents=True)
            (repo / 'go.mod').write_text('module framework\ngo 1.2.3\n')
            (fixture / 'go.mod').write_text('module fixture\ngo 1.0\nreplace framework => ../../..\n')
            (fixture / 'go.sum').write_text('checksum fixture\n')
            with patch.object(synthetic, 'run') as run:
                synthetic.generate(target, repo, synthetic.schema(2, 1, 1, 1))
            generate = [c.args[0] for c in run.call_args_list if 'generate' in c.args[0]]
            self.assertEqual(len(generate), 2)
            self.assertEqual(generate[0][-1], target / 'old')
            self.assertEqual(generate[1][-1], target / 'new')
            self.assertIn('go 1.2.3', (target / 'old/go.mod').read_text())
            self.assertEqual((target / 'new/go.mod').read_text(), (target / 'old/go.mod').read_text())
            with self.assertRaises(FileExistsError):
                synthetic.generate(target, repo, synthetic.schema(2, 1))

    def test_symbols_and_package_totals(self):
        self.assertEqual(synthetic.archive_functions('000 T f\n000 t g\n000 D value\n U import\n'.splitlines()), 2)
        entries = [{'package': synthetic.MODULE + '/handlers/one', 'archive_bytes': 20, 'functions': 3, 'compile_rss_bytes': 40},
                   {'package': synthetic.MODULE + '/models', 'archive_bytes': 30, 'functions': 4, 'compile_rss_bytes': 60}]
        self.assertEqual(synthetic.totals(entries, synthetic.MODULE + '/handlers'),
                         {'packages': 1, 'archive_bytes': 20, 'functions': 3, 'max_compile_rss_bytes': 40})

    def test_sequential_measurement_and_cold_cache_refusal(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            target, repo, cache = root / 'app', root / 'repo', root / 'cache'
            repo.mkdir()
            for style in ('old', 'new'):
                (target / style).mkdir(parents=True)
            synthetic.write_json(target / 'synthetic.json', {'format': 1, 'repo': str(repo),
                                 'spec': synthetic.schema(2, 1, 1, 1), 'sources': {'old': {}, 'new': {}}})
            order = []

            def fake_run(argv, cwd, env, logs, label, timeout=1800):
                order.append(label)
                result = {'seconds': 1.25, 'exit': 0}
                if label == 'toolchain':
                    return 'go fixture', result
                if label.endswith('-build'):
                    style = label.split('-')[0]
                    synthetic.write_json(target / f'measurement/{style}-compile/1.json',
                                         {'package': synthetic.MODULE + '/models', 'rss_bytes': 1024, 'exit': 0})
                    Path(env['GOCACHE']).mkdir()
                    (Path(env['GOCACHE']) / 'archive').write_bytes(b'cache')
                if label.endswith('-exports'):
                    archive = root / 'archive'
                    archive.write_bytes(b'archive')
                    return synthetic.MODULE + '/models\t' + str(archive), result
                return '', result

            def fake_symbols(argv, cwd, env, logs, label, timeout=1800):
                order.append(label)
                return 1

            with patch.object(synthetic.platform, 'system', return_value='Darwin'), patch.object(synthetic, 'run', side_effect=fake_run), \
                    patch.object(synthetic, 'stream_functions', side_effect=fake_symbols):
                output = synthetic.measure(target, cache)
                with self.assertRaises(FileExistsError):
                    synthetic.measure(target, cache)
            report = json.loads((output / 'measurement.json').read_text())
            self.assertTrue(report['complete'])
            self.assertEqual(order, ['toolchain', 'old-build', 'old-exports', 'old-nm-0', 'new-build', 'new-exports', 'new-nm-0'])
            self.assertEqual(report['profiles']['new']['models']['functions'], 1)
            self.assertIn('not a statistical speed comparison', (output / 'summary.md').read_text())

    @unittest.skipUnless(sys.platform == 'darwin', 'native macOS compiler RSS wrapper')
    def test_resource_wrapper_retains_package_rss_and_failed_exit(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            compiler = root / 'compile'
            compiler.write_text('#!/bin/sh\nexit 7\n')
            compiler.chmod(0o700)
            code = synthetic.tool_wrapper([str(compiler), '-p', 'fixture/package'], root)
            self.assertEqual(code, 7)
            records = list(root.glob('*.json'))
            self.assertEqual(len(records), 1)
            record = json.loads(records[0].read_text())
            self.assertEqual(record['package'], 'fixture/package')
            self.assertEqual(record['exit'], 7)
            self.assertGreater(record['rss_bytes'], 0)

    def test_child_failure_timeout_and_output_are_bounded(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            for label, code, timeout, error in (
                ('failure', 'raise SystemExit(7)', 5, RuntimeError),
                ('timeout', 'import time; time.sleep(10)', 0.05, TimeoutError),
                ('output', 'print("x"*2048)', 5, RuntimeError),
            ):
                with patch.object(synthetic, 'OUTPUT_LIMIT', 1024), self.assertRaises(error):
                    synthetic.run([sys.executable, '-c', code], root, os.environ.copy(), root, label, timeout)
                record = json.loads((root / label / 'result.json').read_text())
                self.assertNotEqual(record['exit'], None)
            self.assertEqual(json.loads((root / 'failure/result.json').read_text())['exit'], 7)

    def test_streamed_symbols_exceed_capture_limit_and_stay_bounded(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name).resolve()
            listing = 'import sys; [sys.stdout.write("000 T f%d\\n000 D v\\n" % i) for i in range(500)]'
            with patch.object(synthetic, 'OUTPUT_LIMIT', 1024):
                self.assertEqual(synthetic.stream_functions([sys.executable, '-c', listing], root, os.environ.copy(), root, 'large', 5), 500)
                self.assertFalse((root / 'large/stdout.log').exists())
                self.assertEqual(json.loads((root / 'large/result.json').read_text())['functions'], 500)
                for label, code, timeout, error in (
                    ('failure', 'raise SystemExit(7)', 5, RuntimeError),
                    ('timeout', 'import time; time.sleep(10)', 0.05, TimeoutError),
                    ('stderr', 'import sys; sys.stderr.write("x"*2048)', 5, RuntimeError),
                ):
                    with self.assertRaises(error):
                        synthetic.stream_functions([sys.executable, '-c', code], root, os.environ.copy(), root, label, timeout)
                    self.assertNotEqual(json.loads((root / label / 'result.json').read_text())['exit'], None)


if __name__ == '__main__':
    unittest.main()
