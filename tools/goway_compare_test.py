import importlib.util
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch
import unittest

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location('goway_compare', Path(__file__).with_name('goway_compare.py'))
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


class CompareTests(unittest.TestCase):
    def test_alternation_and_fixed_environment(self):
        self.assertEqual(compare.schedule(5), [(i, s) for i in range(1, 6) for s in ('baseline', 'branch')])
        self.assertEqual(compare.FIXED_ENV['GOFLAGS'], '-p=4')
        self.assertEqual(compare.FIXED_ENV['GOWORK'], 'off')
        self.assertEqual(compare.FIXED_ENV['GOMAXPROCS'], '4')
        self.assertTrue(Path(compare.FIXED_ENV['GOCACHE']).is_absolute())
        if 'GOCACHE' in os.environ:
            self.assertEqual(compare.FIXED_ENV['GOCACHE'], os.environ['GOCACHE'])

    def test_stage6_plan_selects_exact_baseline_and_no_database(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            for side in ('baseline', 'branch'):
                (root / side / 'tests/fixtures/goway').mkdir(parents=True)
            args = SimpleNamespace(baseline=root / 'baseline', branch=root / 'branch',
                                   output=root / 'plan', mode='stage6', baseline_ref='main',
                                   trials=5, benchtime='1s', go='go')
            def git(repo, *command):
                if command[0] == 'status':
                    return ''
                return '35f8d45-full' if repo.name == 'baseline' else 'branch-full'
            with patch.object(compare, 'git', side_effect=git), patch.object(compare.synthetic, 'run') as run:
                _, _, plan = compare.prepare(args)
            run.assert_not_called()
            self.assertEqual(plan['baseline_ref'], '35f8d45')
            self.assertEqual(len(plan['commands']), 12)
            self.assertEqual(plan['commands'][0]['argv'][-1], './bench')
            for command in plan['commands'][2:]:
                self.assertIn('-test.run=^$', command['argv'])
                self.assertIn('-test.bench=^Benchmark(ChainBuild|ChainCompile|Hydrate1000)$', command['argv'])
            self.assertFalse(plan['complete'])

    def test_samples_keep_allocation_and_work_metrics(self):
        sample = compare.bench_samples('BenchmarkHydrate1000-4 10 110 ns/op 12 B/op 3 allocs/op 1000 rows/op 1 queries/op\n')
        self.assertEqual(sample['BenchmarkHydrate1000']['rows/op'], 1000)
        self.assertEqual(sample['BenchmarkHydrate1000']['allocs/op'], 3)
        with self.assertRaises(ValueError):
            compare.bench_samples('BenchmarkChainBuild-4 10 5 ns/op\n')

    def test_footprints_keep_migrated_probe_identity(self):
        before = compare.footprint_samples('probe model_draft: 225 functions (max 282), 1566 symbols (max 1958), 991000 bytes (max 1239298)')
        after = compare.footprint_samples('probe model_assignments: 233 functions (max 268), 1647 symbols (max 1895), 1060408 bytes (max 1219470)')
        goway = compare.footprint_samples('probe q_with: 1211 functions (baseline 1100, max 1211), 400000 archive bytes')
        result = compare.footprint_summary([{'name': name, 'probes': probe} for name, probe in
            [('baseline-consumer', before), ('branch-consumer', after), ('branch-goway', goway)]])
        self.assertFalse(result['consumer']['model_assignments']['same_probe_name'])
        self.assertEqual(result['branch_only_goway']['q_with']['functions'], 1211)
        with self.assertRaises(ValueError):
            compare.footprint_samples('PASS')

    def test_summary_requires_all_workloads_in_every_trial(self):
        samples = []
        for trial, side in compare.schedule(5):
            metrics = {name: {'ns/op': 10 + trial, 'B/op': 20, 'allocs/op': 2}
                       for name in ('BenchmarkChainBuild', 'BenchmarkChainCompile', 'BenchmarkHydrate1000')}
            samples.append({'side': side, 'benchmarks': metrics})
        report = compare.summary(samples, 'stage6', 5)
        self.assertEqual(report['BenchmarkChainCompile']['median_change_percent'], 0)
        self.assertTrue(report['BenchmarkChainCompile']['ranges_overlap'])
        self.assertEqual(report['BenchmarkChainCompile']['baseline']['ns/op']['median'], 13)
        samples[-1]['benchmarks'].pop('BenchmarkHydrate1000')
        with self.assertRaises(ValueError):
            compare.summary(samples, 'stage6', 5)


if __name__ == '__main__':
    unittest.main()
