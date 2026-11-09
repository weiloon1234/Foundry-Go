#!/usr/bin/env python3
"""Run a phase unchanged, retaining safe context before its traceback is lost."""
from pathlib import Path
import runpy
import sys


def main():
    script = Path(sys.argv[1]).resolve(strict=True)
    sys.argv = sys.argv[1:]
    sys.path.insert(0, str(script.parent))
    try:
        runpy.run_path(str(script), run_name='__main__')
    except BaseException as error:
        # Import only after the phase selected its own (possibly staged) bundle.
        try:
            from acceptance_diagnostics import artisan_failure
            artisan_failure(error)
        except Exception:
            print('ARTISAN FAILURE DIAGNOSTICS: unavailable', flush=True)
        raise


if __name__ == '__main__':
    main()
