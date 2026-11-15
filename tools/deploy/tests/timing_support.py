"""Controlled monotonic time and worker checkpoints for concurrent deadline tests."""
from contextlib import contextmanager
import threading
import time


class ManualClock:
    def __init__(self, start=1000):
        self.now = start

    def monotonic(self):
        return self.now

    def advance(self, seconds):
        assert seconds >= 0
        self.now += seconds

    def __getattr__(self, name):
        # Patch a consumer's time reference, never the shared time module used
        # by threading or by the test's real-time deadlock guards.
        return getattr(time, name)


class Checkpoints:
    def __init__(self):
        self.condition = threading.Condition()
        self.reached = set()
        self.released = set()
        self.closed = False

    def hold(self, key):
        with self.condition:
            self.reached.add(key)
            self.condition.notify_all()
            assert self.condition.wait_for(lambda: self.closed or key in self.released, 10), f"Unreleased checkpoint: {key}"

    def wait(self, *keys):
        with self.condition:
            assert self.condition.wait_for(lambda: self.closed or set(keys) <= self.reached, 10), f"Missing checkpoints: {set(keys) - self.reached}"
            assert not self.closed, "Checkpoint controller closed early"

    def release(self, *keys):
        with self.condition:
            self.released.update(keys)
            self.condition.notify_all()

    @contextmanager
    def drive(self, clock, scenario):
        errors = []
        def run():
            try:
                scenario()
            except BaseException as error:
                errors.append(error)
                clock.advance(1000)
            finally:
                with self.condition:
                    self.closed = True
                    self.condition.notify_all()
        driver = threading.Thread(target=run, name="readiness-test-controller")
        driver.start()
        try:
            yield
        finally:
            with self.condition:
                self.closed = True
                self.condition.notify_all()
            driver.join(10)
            assert not driver.is_alive(), "Checkpoint controller did not exit"
        if errors:
            raise errors[0]
