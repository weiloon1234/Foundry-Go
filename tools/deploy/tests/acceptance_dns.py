"""Managed DNS container fault scenario. Uses only the loopback controlled API."""
import os
import unittest


def run():
    if os.environ.get("FOUNDRY_DISPOSABLE_HOST") != "1":
        raise RuntimeError("Managed DNS container scenarios require a disposable host")
    import test_dns
    suite = unittest.defaultTestLoader.loadTestsFromModule(test_dns)
    result = unittest.TextTestRunner(verbosity=1).run(suite)
    if not result.wasSuccessful():
        raise RuntimeError("Controlled managed DNS scenarios failed")


if __name__ == "__main__":
    run()
