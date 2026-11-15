"""Repeat native readiness tests in four processes with bounded background CPU load.

Example: python3 -B readiness_stress.py --repeat 50 --output /tmp/readiness-stress \
    test_feature_maintenance test_services test_copies
"""
import argparse
from concurrent.futures import ProcessPoolExecutor
from contextlib import ExitStack
import io
import json
import multiprocessing
from pathlib import Path
import threading
import time
import unittest
from unittest.mock import patch


def burn(stop):
    while not stop.is_set():
        sum(n * n for n in range(10000))


def worker(index, repetitions, names, delay, output):
    import test_feature_wiring as wiring
    import services
    original = wiring.WiringHost.run
    ready = services.wait_ready_set
    lock = threading.Lock()

    def delayed(host, args, **kwargs):
        with lock:
            stall = (threading.current_thread().name.startswith("foundry-ready")
                     and getattr(host, "stress_final_check", False)
                     and not getattr(host, "stress_delayed", False) and args[0] == "ss")
            if stall:
                host.stress_delayed = True
        if stall:
            time.sleep(delay)
        return original(host, args, **kwargs)

    def final_check(host, *args, **kwargs):
        host.stress_final_check = not kwargs.get("starting", False)
        try:
            return ready(host, *args, **kwargs)
        finally:
            host.stress_final_check = False

    counts, failures = {}, []
    def flatten(suite):
        for test in suite:
            if isinstance(test, unittest.TestSuite):
                yield from flatten(test)
            else:
                yield test

    with ExitStack() as stack:
        if delay:
            stack.enter_context(patch.object(wiring.WiringHost, "run", delayed))
            stack.enter_context(patch.object(services, "wait_ready_set", final_check))
        for iteration in range(repetitions):
            for test in flatten(unittest.defaultTestLoader.loadTestsFromNames(names)):
                stream = io.StringIO()
                result = unittest.TextTestRunner(stream=stream).run(test)
                counts[test.id()] = counts.get(test.id(), 0) + 1
                if not result.wasSuccessful() or result.skipped:
                    failures.append({"test": test.id(), "iteration": iteration, "output": stream.getvalue()})
                progress = Path(output, f"worker-{index}.tmp")
                progress.write_text(json.dumps({"counts": counts, "failures": failures}, indent=2))
                progress.replace(Path(output, f"worker-{index}.json"))
    return counts, failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repeat", type=int, default=50)
    parser.add_argument("--delay", type=float, default=0)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("tests", nargs="+")
    args = parser.parse_args()
    assert args.repeat >= 4 and args.delay >= 0
    args.output.mkdir(parents=True, exist_ok=False)
    context = multiprocessing.get_context("spawn")
    stop = context.Event()
    load = [context.Process(target=burn, args=(stop,)) for _ in range(2)]
    for process in load:
        process.start()
    started = time.monotonic()
    try:
        with ProcessPoolExecutor(max_workers=4, mp_context=context) as pool:
            futures = [pool.submit(worker, index, args.repeat // 4 + (index < args.repeat % 4),
                                   args.tests, args.delay, str(args.output)) for index in range(4)]
            results = [future.result(timeout=7200) for future in futures]
    finally:
        stop.set()
        for process in load:
            process.join(10)
            if process.is_alive():
                process.terminate()
                process.join(10)
            assert not process.is_alive()
    counts, failures = {}, []
    for observed, errors in results:
        for name, count in observed.items():
            counts[name] = counts.get(name, 0) + count
        failures.extend(errors)
    report = {"workers": 4, "cpu_load_processes": 2, "delay": args.delay,
              "seconds": round(time.monotonic() - started, 2), "counts": counts, "failures": failures}
    (args.output / "result.json").write_text(json.dumps(report, indent=2))
    print(json.dumps({**report, "counts": len(counts), "runs": sum(counts.values()), "failures": len(failures)}), flush=True)
    return bool(failures)


if __name__ == "__main__":
    raise SystemExit(main())
