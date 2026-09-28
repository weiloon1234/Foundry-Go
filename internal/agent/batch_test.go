package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func batchFixture(t *testing.T) ([]Options, string) {
	t.Helper()
	base, source := inspectionFixture(t)
	var options []Options
	for i, operation := range []string{"complete", "hover", "definition"} {
		item := base
		item.Operation, item.Insert = operation, []string{"X", "Y", "Z"}[i]
		options = append(options, item)
	}
	return options, source
}

func TestBatchInspectionsShareOnlyTheirScenarioAndKeepIndependentOverlays(t *testing.T) {
	options, source := batchFixture(t)
	for range 2 {
		// The helper demands one initialization followed by all three ordered
		// open/request/close operations before shutdown, in each fresh process.
		results, err := inspectBatch(t.Context(), options, 3*time.Second, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch"})
		if err != nil || len(results) != len(options) {
			t.Fatal(len(results), err)
		}
		for i, result := range results {
			if result.Operation != options[i].Operation || result.Server.Name != "protocol-fixture" || result.Position != (Position{1, 6}) {
				t.Fatalf("batch result: %+v", result)
			}
			if result.Timing.InitializeNanoseconds <= 0 || result.Timing.RequestNanoseconds <= 0 || result.Timing.InitializeNanoseconds != results[0].Timing.InitializeNanoseconds {
				t.Fatal("batch lost its shared initialization or per-operation timing", result.Timing)
			}
		}
	}
	if data, err := os.ReadFile(options[0].File); err != nil || string(data) != source {
		t.Fatal("batch edited source", err)
	}
}

func TestBatchPhaseDeadlinesResetAndFailuresStopLaterOperations(t *testing.T) {
	options, _ := batchFixture(t)
	results, err := inspectBatch(t.Context(), options, 900*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch-slow"})
	if err != nil || len(results) != 3 {
		t.Fatal("operation deadlines were not independent", len(results), err)
	}
	for _, mode := range []string{"batch-error", "batch-hang"} {
		started := time.Now()
		results, err := inspectBatch(t.Context(), options, 500*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", mode})
		if err == nil || len(results) != 1 || !strings.Contains(err.Error(), "operation 2 (hover)") {
			t.Fatal("batch lost its completed prefix or failed phase", len(results), err)
		}
		if mode == "batch-hang" && (!errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second) {
			t.Fatal("batch cancellation was lost or cleanup hung", err)
		}
	}
}

func TestBatchInitializationHasItsOwnDeadline(t *testing.T) {
	options, _ := batchFixture(t)
	started := time.Now()
	results, err := inspectBatch(t.Context(), options, 500*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "hang"})
	if !errors.Is(err, context.DeadlineExceeded) || len(results) != 0 || time.Since(started) > 3*time.Second {
		t.Fatal("unbounded batch initialization", len(results), err)
	}
}

func TestBatchValidatesAllRequestsBeforeStartingServer(t *testing.T) {
	options, _ := batchFixture(t)
	executable := filepath.Join(t.TempDir(), "absent")
	for i := range options {
		options[i].Gopls = executable
	}
	for _, mutate := range []func([]Options){
		func(items []Options) { items[2].Operation = "unknown" },
		func(items []Options) { items[2].Gopls = "different" },
		func(items []Options) {
			other, _ := inspectionFixture(t)
			items[2].Workspace, items[2].File = other.Workspace, other.File
		},
	} {
		items := append([]Options(nil), options...)
		mutate(items)
		_, err := InspectBatch(t.Context(), items, time.Second)
		if err == nil || strings.Contains(err.Error(), "start gopls") {
			t.Fatal("invalid batch started a server", err)
		}
	}
	for _, items := range [][]Options{nil, make([]Options, MaxBatchOperations+1)} {
		if _, err := InspectBatch(t.Context(), items, time.Second); err == nil {
			t.Fatal("unbounded or empty batch accepted")
		}
	}
	if _, err := InspectBatch(t.Context(), options, 0); err == nil {
		t.Fatal("unbounded operation accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := InspectBatch(ctx, options, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled batch started", err)
	}
}
