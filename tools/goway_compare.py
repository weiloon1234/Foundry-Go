#!/usr/bin/env python3
"""Plan (default) or run same-session, alternating Go-way acceptance comparisons.

No checkout creation, generation, credentials or Git writes. Supply prepared trees.
Use --run only on a quiet host; dry plans do not run any Go commands.
"""

import argparse
import hashlib
import math
import os
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess
import sys

import goway_synthetic as synthetic

ROOT = Path(__file__).resolve().parents[1]
FIXED_ENV = {'GOCACHE': os.environ.get('GOCACHE', '/private/tmp/foundry-goway-codex-gocache-r18'),
             'GOMAXPROCS': '4', 'GOFLAGS': '-p=4', 'GOWORK': 'off',
             'GOENV': 'off', 'GOTOOLCHAIN': 'local',
             'GOGC': '100', 'GOMEMLIMIT': 'off', 'GODEBUG': ''}
BENCHES = {'orm': '^BenchmarkORM',
           'stage6': '^Benchmark(ChainBuild|ChainCompile|Hydrate1000)$'}
BENCH_LINE = re.compile(r'^(Benchmark\S+)-\d+\s+(\d+)\s+(.+)$')


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()


def schedule(trials):
    # Adjacent A/B trials; both variants start fresh processes each time.
    return [(trial, side) for trial in range(1, trials + 1) for side in ('baseline', 'branch')]


def module(repo, mode):
    return repo / ('tests/fixtures/goway' if mode == 'stage6' else 'tests/fixtures/consumer')


def bench_samples(output):
    result = {}
    for line in output.splitlines():
        match = BENCH_LINE.match(line)
        if not match:
            continue
        name, iterations, fields = match.groups()
        tokens = fields.split()
        if len(tokens) % 2 or name in result or int(iterations) < 1:
            raise ValueError('invalid or repeated benchmark sample')
        result[name] = {tokens[i + 1]: float(tokens[i]) for i in range(0, len(tokens), 2)}
        if (not {'ns/op', 'B/op', 'allocs/op'} <= result[name].keys() or
                any(not math.isfinite(v) or v < 0 for v in result[name].values()) or
                result[name].get('ns/op', 0) == 0):
            raise ValueError('missing benchmark metrics')
    return result


def footprint_samples(output):
    pattern = re.compile(r'probe (\w+): (\d+) functions \([^)]*\), '
                         r'(?:(\d+) symbols \([^)]*\), )?(\d+) (?:archive )?bytes')
    result = {}
    for name, functions, symbols, size in pattern.findall(output):
        if name in result:
            raise ValueError('repeated footprint probe')
        result[name] = {'functions': int(functions), 'archive_bytes': int(size)}
        if symbols:
            result[name]['defined_symbols'] = int(symbols)
    if not result:
        raise ValueError('footprint gate produced no probe measurements')
    return result


def footprint_summary(samples):
    by_run = {s['name']: s['probes'] for s in samples}
    before, after = by_run['baseline-consumer'], by_run['branch-consumer']
    result = {}
    for name, current in after.items():
        old_name = 'model_draft' if name == 'model_assignments' else name
        if old_name not in before:
            raise ValueError('consumer footprint baseline lacks ' + old_name)
        result[name] = {'baseline_probe': old_name, 'branch_probe': name,
                        'baseline': before[old_name], 'branch': current,
                        'same_probe_name': old_name == name}
    return {'consumer': result, 'branch_only_goway': by_run['branch-goway']}


def summary(samples, mode, trials):
    expected = ({'BenchmarkChainBuild', 'BenchmarkChainCompile', 'BenchmarkHydrate1000'}
                if mode == 'stage6' else
                {'BenchmarkORMCompile/list', 'BenchmarkORMCompile/filters'} |
                {f'BenchmarkORMPostgresReads/{n}/{s}' for n in (1, 100, 1000)
                 for s in ('direct-sql', 'framework-sql', 'typed-orm')})
    if (len(samples) != trials * 2 or any(set(s['benchmarks']) != expected for s in samples) or
            any(sum(s['side'] == side for s in samples) != trials for side in ('baseline', 'branch'))):
        raise ValueError('missing/extra benchmark workload; refusing incomplete comparison')
    result = {}
    for name in sorted(expected):
        entry = {}
        for side in ('baseline', 'branch'):
            runs = [s['benchmarks'][name] for s in samples if s['side'] == side]
            entry[side] = {unit: {'median': statistics.median(r[unit] for r in runs),
                                  'min': min(r[unit] for r in runs), 'max': max(r[unit] for r in runs)}
                           for unit in runs[0]}
        before, after = entry['baseline']['ns/op'], entry['branch']['ns/op']
        entry['median_change_percent'] = (after['median'] / before['median'] - 1) * 100
        entry['ranges_overlap'] = after['min'] <= before['max'] and before['min'] <= after['max']
        entry['timing_verdict'] = 'review load, dispersion and work equivalence; median alone is not a ±5% gate'
        result[name] = entry
    return result


def prepare(args):
    repos = {'baseline': args.baseline.resolve(), 'branch': args.branch.resolve()}
    output = args.output.resolve()
    for repo in repos.values():
        synthetic.outside(output, repo)
        if not module(repo, args.mode).is_dir():
            raise ValueError(f'missing {args.mode} fixture in {repo}')
    ref = '35f8d45' if args.mode == 'stage6' else args.baseline_ref
    if git(repos['baseline'], 'rev-parse', 'HEAD') != git(repos['baseline'], 'rev-parse', ref):
        raise ValueError(f'baseline must be checked out at {ref}')
    if git(repos['baseline'], 'status', '--porcelain'):
        raise ValueError('baseline checkout must be clean')
    plan = {'format': 1, 'mode': args.mode, 'baseline_ref': ref, 'environment': FIXED_ENV,
            'platform': platform.platform(), 'cpu_count': os.cpu_count(),
            'load_average': os.getloadavg(),
            'trials': 1 if args.mode == 'footprint' else args.trials,
            'benchtime': None if args.mode == 'footprint' else args.benchtime,
            'repos': {s: {'path': str(r), 'commit': git(r, 'rev-parse', 'HEAD'),
                          'dirty': bool(git(r, 'status', '--porcelain')),
                          'sources': synthetic.fingerprint(r)} for s, r in repos.items()},
            'harness_sources': {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                for p in (Path(__file__), Path(synthetic.__file__))},
            'commands': [], 'samples': [], 'complete': False}
    if args.mode != 'footprint':
        for side, repo in repos.items():
            binary = output / f'{side}.test'
            plan['commands'].append({'name': f'{side}-build', 'cwd': str(module(repo, args.mode)),
                'argv': [args.go, 'test', '-c', '-o', str(binary), './bench' if args.mode == 'stage6' else '.']})
        for trial, side in schedule(args.trials):
            plan['commands'].append({'name': f'{trial:02d}-{side}', 'side': side, 'trial': trial,
                'cwd': str(module(repos[side], args.mode)),
                'argv': [str(output / f'{side}.test'), '-test.timeout=30m',
                         '-test.run=^TestORMBenchmarkReadsAgree$' if args.mode == 'orm' else '-test.run=^$',
                         '-test.bench=' + BENCHES[args.mode], '-test.benchmem',
                         '-test.benchtime=' + args.benchtime, '-test.count=1']})
    else:
        for side, repo in repos.items():
            plan['commands'].append({'name': side + '-consumer', 'side': side,
                'cwd': str(module(repo, args.mode)),
                'argv': [args.go, 'test', '-count=1', '-v', '-run=^TestFootprintBudgets$', '.']})
        plan['commands'].append({'name': 'branch-goway', 'side': 'branch',
            'cwd': str(repos['branch'] / 'tests/fixtures/goway'),
            'argv': [args.go, 'test', '-count=1', '-v', '-run=^TestFootprintBudgets$', '.']})
        plan['comparability'] = 'Compare common consumer probes; model_draft versus model_assignments is an API migration, not identical source. Goway probes are branch-only; do not invent a main baseline.'
    output.mkdir(parents=True, exist_ok=False)
    synthetic.write_json(output / 'comparison.json', plan)
    return repos, output, plan


def execute(args, repos, output, plan):
    env = os.environ.copy()
    env.update(FIXED_ENV)
    if args.mode == 'orm':
        env['FOUNDRY_TEST_POSTGRES_REQUIRED'] = '1'
    logs = output / 'logs'
    plan['toolchain'] = synthetic.run([args.go, 'version'], ROOT, env, logs, 'toolchain')[0].strip()
    for command in plan['commands']:
        start = os.getloadavg()
        text, record = synthetic.run(command['argv'], command['cwd'], env, logs, command['name'], 3600)
        if 'side' in command:
            sample = {'side': command['side'], 'name': command['name'],
                      'load_average_start': start, 'load_average_end': os.getloadavg(), 'command': record}
            if args.mode != 'footprint':
                sample['benchmarks'] = bench_samples(text)
            else:
                sample['probes'] = footprint_samples(text)
            plan['samples'].append(sample)
            synthetic.write_json(output / 'comparison.json', plan)
    for side, repo in repos.items():
        if synthetic.fingerprint(repo) != plan['repos'][side]['sources']:
            raise ValueError(f'{side}: source changed during measurement')
    if args.mode != 'footprint':
        plan['summary'] = summary(plan['samples'], args.mode, args.trials)
    else:
        plan['summary'] = footprint_summary(plan['samples'])
    plan['complete'] = True
    synthetic.write_json(output / 'comparison.json', plan)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('orm', 'stage6', 'footprint'))
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--baseline-ref', default='main', help='pinned main revision; stage6 always uses 35f8d45')
    parser.add_argument('--branch', type=Path, default=ROOT)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--trials', type=int, default=5)
    parser.add_argument('--benchtime', default='1s')
    parser.add_argument('--go', default='go')
    parser.add_argument('--run', action='store_true')
    args = parser.parse_args()
    if args.trials < 5 or not re.fullmatch(r'[1-9][0-9]*(ns|us|ms|s|m|x)', args.benchtime):
        parser.error('at least five trials and an explicit positive benchmark duration/count are required')
    repos, output, plan = prepare(args)
    if args.run:
        execute(args, repos, output, plan)
    print(output / 'comparison.json')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, RuntimeError, OSError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
