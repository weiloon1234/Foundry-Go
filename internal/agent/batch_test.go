package agent

import (
	"context"
	"errors"
	"fmt"
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
		results, err := inspectBatch(t.Context(), options, 3*time.Second, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch"}, shutdownGrace)
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
	results, err := inspectBatch(t.Context(), options, 900*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch-slow"}, shutdownGrace)
	if err != nil || len(results) != 3 {
		t.Fatal("operation deadlines were not independent", len(results), err)
	}
	for _, mode := range []string{"batch-error", "batch-hang"} {
		started := time.Now()
		results, err := inspectBatch(t.Context(), options, 500*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", mode}, shutdownGrace)
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
	results, err := inspectBatch(t.Context(), options, 500*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "hang"}, shutdownGrace)
	if !errors.Is(err, context.DeadlineExceeded) || len(results) != 0 || time.Since(started) > 3*time.Second {
		t.Fatal("unbounded batch initialization", len(results), err)
	}
}

func TestBatchShutdownWaitsForBackgroundWork(t *testing.T) {
	options, _ := batchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	// All operations finish promptly, but the server must finish background
	// work before acknowledging shutdown. This exceeded the old five-second
	// cleanup timer despite remaining inside the caller's deadline.
	results, err := inspectBatch(ctx, options, time.Second, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch-slow-shutdown"}, shutdownGrace)
	if err != nil || len(results) != len(options) {
		t.Fatalf("completed batch failed during responsive shutdown: %d results, %v", len(results), err)
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
		_, err := inspectTestBatch(t.Context(), items, time.Second)
		if err == nil || strings.Contains(err.Error(), "start gopls") {
			t.Fatal("invalid batch started a server", err)
		}
	}
	for _, items := range [][]Options{nil, make([]Options, MaxBatchOperations+1)} {
		if _, err := inspectTestBatch(t.Context(), items, time.Second); err == nil {
			t.Fatal("unbounded or empty batch accepted")
		}
	}
	if _, err := inspectTestBatch(t.Context(), options, 0); err == nil {
		t.Fatal("unbounded operation accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := inspectTestBatch(ctx, options, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled batch started", err)
	}
}

// inspectTestBatch owns one fresh language-server session for a related scenario.
// Each operation opens its independently prepared overlay and closes it before
// the next operation. Initialization and each operation have separate deadlines;
// any failure stops the batch and closes the owned process. Returned results are
// the completed prefix; callers must still check the error, including cleanup.
// No server or unsaved state is shared between calls.
func inspectTestBatch(ctx context.Context, options []Options, timeout time.Duration) ([]Result, error) {
	if len(options) == 0 || len(options) > MaxBatchOperations || timeout <= 0 {
		return nil, fmt.Errorf("agent batch requires bounded operations and a positive operation timeout")
	}
	executable := options[0].Gopls
	if executable == "" {
		executable = "gopls"
	}
	for _, item := range options {
		selected := item.Gopls
		if selected == "" {
			selected = "gopls"
		}
		if selected != executable {
			return nil, fmt.Errorf("agent batch must use one language-server executable")
		}
	}
	return inspectBatch(ctx, options, timeout, executable, []string{"serve"}, shutdownGrace)
}
