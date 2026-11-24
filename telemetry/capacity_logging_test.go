package telemetry

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestRunCapacityWarningsAreBoundedAndRecover(t *testing.T) {
	var logs bytes.Buffer
	now := time.Now()
	e := &Engine{collector: &Collector{elapsed: func() time.Time { return now }}, deps: Dependencies{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}}
	refused := safeerror.Prefix("telemetry storage", errRunCapacity)
	if !errors.Is(refused, fault.Overloaded) {
		t.Fatal("capacity diagnostic lost overloaded classification")
	}
	e.logFailure(refused)
	e.logFailure(refused)
	now = now.Add(time.Hour - time.Nanosecond)
	e.logFailure(refused)
	if got := strings.Count(logs.String(), `"level":"WARN"`); got != 1 {
		t.Fatalf("warnings before interval = %d", got)
	}
	now = now.Add(time.Nanosecond)
	e.logFailure(refused)
	if got := strings.Count(logs.String(), `"level":"WARN"`); got != 2 {
		t.Fatalf("warnings at interval = %d", got)
	}
	e.logCapacityRecovery()
	e.logCapacityRecovery()
	if strings.Count(logs.String(), "telemetry run capacity recovered") != 1 || !strings.Contains(logs.String(), `"refusals":4`) {
		t.Fatal("missing or repeated counted recovery notice")
	}
	e.logFailure(refused)
	e.logFailure(safeerror.NewFault(fault.Overloaded, "telemetry receipt capacity reached"))
	if strings.Count(logs.String(), `"level":"WARN"`) != 4 {
		t.Fatal("new refusal or unrelated error was suppressed")
	}
	if !strings.Contains(logs.String(), "telemetry run capacity reached") || !strings.Contains(logs.String(), "diagnostic") {
		t.Fatal("warning lost its safe diagnostic")
	}
}
