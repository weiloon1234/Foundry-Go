package telemetry_test

import (
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/telemetry"
	telemetrytest "github.com/weiloon1234/Foundry-Go/testkit/telemetry"
)

var _ func(testing.TB, *telemetry.Engine) = telemetrytest.Flush

type flushFailure struct {
	testing.TB
	helper  bool
	message string
}

func (f *flushFailure) Helper()           { f.helper = true }
func (f *flushFailure) Fatal(args ...any) { f.message = fmt.Sprint(args...) }

func TestFlushFailsTestOnEngineError(t *testing.T) {
	for _, engine := range []*telemetry.Engine{nil, {}} {
		failure := &flushFailure{TB: t}
		telemetrytest.Flush(failure, engine)
		if !failure.helper || failure.message != "flush test telemetry failed" {
			t.Fatalf("flush did not report a safe helper failure: %+v", failure)
		}
	}
}
