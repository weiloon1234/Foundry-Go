// Package testcleanup shares the retention rule for test-owned backend data.
package testcleanup

import (
	"os"
	"runtime"
	"testing"
)

// Keep reports whether a test has failed, is unwinding a panic or FailNow, or
// explicitly retains data. A skipped test that did not fail releases its data.
// Call it during cleanup, after application and client shutdown.
func Keep(t testing.TB, variable string) bool {
	t.Helper()
	if t.Failed() || os.Getenv(variable) == "1" {
		return true
	}
	// SkipNow unwinds through runtime.Goexit; a skip is not a failure.
	if t.Skipped() {
		return false
	}
	// testing runs cleanups while unwinding a panic, before marking the test
	// failed. Inspect function names only; never recover or format its payload.
	// An incomplete stack cannot establish success, so retain conservatively.
	var pcs [128]uintptr
	n := runtime.Callers(2, pcs[:])
	if n == len(pcs) {
		return true
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == "runtime.gopanic" || frame.Function == "runtime.Goexit" {
			return true
		}
		if !more {
			return false
		}
	}
}
