// Package telemetry provides synchronous persistence helpers for telemetry tests.
package telemetry

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/telemetrytest"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// Flush captures once and drains the engine's pending batches in FIFO order,
// failing the test on error. It works before Run or alongside its periodic loop,
// starts no loop, and refuses after Close begins. Normal limits, loss accounting,
// receipts and incident capture counts apply; repeated calls do not duplicate
// persisted observation counts. DrainTimeout bounds cooperative work, while
// callbacks retain ownership until exit. Flush does not wait for asynchronous
// error reporters; ensure their observations have arrived before calling it.
func Flush(t testing.TB, engine *telemetry.Engine) {
	t.Helper()
	if err := engine.Flush(t.Context(), telemetrytest.Grant()); err != nil {
		t.Fatal("flush test telemetry failed")
	}
}
