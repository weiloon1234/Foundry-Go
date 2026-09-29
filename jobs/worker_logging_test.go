package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type unformattableJobError struct{}

func (unformattableJobError) Error() string { panic("private-error-must-not-be-formatted") }

func replaceLoggedWorker(t *testing.T, f workerFixture, handler jobs.Handler[payload], logger *slog.Logger, enabled bool) workerFixture {
	t.Helper()
	declaration, err := f.definition.Declare(handler)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency = 1
	config.PollInterval = time.Millisecond
	config.FailureLog = enabled
	f.worker, err = jobs.NewWorker(f.backend, registry, config, jobs.WithWorkerLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWorkerFailureLoggingHasMetadataWithoutErrorOrPayload(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Attempts = 2
			policy.Backoff = []time.Duration{0}
			policy.Jitter = 0
			handler := func(context.Context, payload) error { return unformattableJobError{} }
			f := newWorkerFixture(t, policy, handler)
			var output bytes.Buffer
			f = replaceLoggedWorker(t, f, handler, slog.New(slog.NewJSONHandler(&output, nil)), enabled)
			receipt, err := f.definition.Dispatch(t.Context(), f.dispatcher, payload{Labels: map[string]string{"secret": "private-payload"}}, jobs.Options[payload]{})
			if err != nil {
				t.Fatal(err)
			}
			runWorker(t, f)
			waitRecord(t, f, receipt.ID, jobs.Failed)
			if err := f.worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if !enabled {
				if text != "" {
					t.Fatal("disabled worker emitted logs")
				}
				return
			}
			if strings.Contains(text, "private") || strings.Contains(text, "payload") {
				t.Fatal("job log contains private data")
			}
			lines := strings.Split(strings.TrimSpace(text), "\n")
			if len(lines) != 2 {
				t.Fatal("expected one log per failed attempt", len(lines))
			}
			for index, line := range lines {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				level := "WARN"
				if index == 1 {
					level = "ERROR"
				}
				if record["job_id"] != receipt.ID.String() || record["attempt"] != float64(index+1) || record["level"] != level || record["reason"] != string(jobs.HandlerFailed) || record["finalized"] != true {
					t.Fatal("incorrect job log metadata", record)
				}
			}
		})
	}
}

type abnormalJobLogHandler struct {
	mode  string
	calls atomic.Int32
}

func (*abnormalJobLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *abnormalJobLogHandler) Handle(context.Context, slog.Record) error {
	h.calls.Add(1)
	if h.mode == "panic" {
		panic("private logger panic")
	}
	runtime.Goexit()
	return nil
}
func (h *abnormalJobLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *abnormalJobLogHandler) WithGroup(string) slog.Handler      { return h }

func TestWorkerLoggingFailureDoesNotReplayOrLoseCapacity(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Attempts = 1
			var calls atomic.Int32
			handler := func(context.Context, payload) error {
				if calls.Add(1) == 1 {
					return unformattableJobError{}
				}
				return nil
			}
			f := newWorkerFixture(t, policy, handler)
			logger := &abnormalJobLogHandler{mode: mode}
			f = replaceLoggedWorker(t, f, handler, slog.New(logger), true)
			first := f.enqueue(t)
			second := f.enqueue(t)
			runWorker(t, f)
			waitRecord(t, f, first, jobs.Failed)
			waitRecord(t, f, second, jobs.Succeeded)
			if err := f.worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 || logger.calls.Load() != 1 || f.worker.Active() != 0 {
				t.Fatal("logger changed job delivery or retained capacity")
			}
		})
	}
}

type blockingJobLogHandler struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*blockingJobLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockingJobLogHandler) Handle(context.Context, slog.Record) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}
func (h *blockingJobLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockingJobLogHandler) WithGroup(string) slog.Handler      { return h }

func TestBlockingJobLoggerRetainsSlotAndShutdownOwnership(t *testing.T) {
	policy := jobs.DefaultPolicy("default")
	policy.Attempts = 1
	handler := func(context.Context, payload) error { return unformattableJobError{} }
	f := newWorkerFixture(t, policy, handler)
	logger := &blockingJobLogHandler{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(logger.release) })
	f = replaceLoggedWorker(t, f, handler, slog.New(logger), true)
	id := f.enqueue(t)
	runWorker(t, f)
	select {
	case <-logger.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("logger did not start")
	}
	waitRecord(t, f, id, jobs.Failed)
	stop, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := f.worker.Stop(stop); err != context.DeadlineExceeded {
		t.Fatal("shutdown abandoned logger", err)
	}
	if f.worker.Active() != 1 {
		t.Fatal("blocked logger slot was released")
	}
	select {
	case <-f.worker.Done():
		t.Fatal("worker closed under logger")
	default:
	}
	release.Do(func() { close(logger.release) })
	if err := f.worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.worker.Active() != 0 {
		t.Fatal("logger did not release slot")
	}
}

func TestJobFinalizationFailureIsLoggedAsUnconfirmed(t *testing.T) {
	handler := func(context.Context, payload) error { return nil }
	f := newWorkerFixture(t, jobs.DefaultPolicy("default"), handler)
	id := f.enqueue(t)
	declaration, err := f.definition.Declare(handler)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := jobs.DefaultWorkerConfig(f.key.Namespace(), f.key.Queue())
	config.Concurrency = 1
	config.LeaseDuration, config.HeartbeatInterval, config.OperationTimeout, config.FailureBackoff = time.Second, 100*time.Millisecond, 300*time.Millisecond, 10*time.Millisecond
	logs := &lockedBuffer{}
	backend := failingWorkerBackend{Backend: f.backend, operation: "finish", failure: unformattableJobError{}}
	worker, err := jobs.NewWorker(backend, registry, config, jobs.WithWorkerLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(t.Context()) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), `"msg":"job completion unconfirmed"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := worker.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal("a failed acknowledgement stopped the worker", err)
	}
	text := logs.String()
	if !strings.Contains(text, `"msg":"job completion unconfirmed"`) || !strings.Contains(text, `"finalized":false`) || !strings.Contains(text, `"diagnostic"`) || strings.Contains(text, "private") {
		t.Fatal("unknown completion was misreported")
	}
	waitRecord(t, f, id, jobs.Running)
}

// lockedBuffer lets a test read logs while the worker is still writing them.
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// Stop interrupts a running handler: the attempt is released without consuming
// the retry budget, even on the final attempt, and is neither logged nor
// observed as a failure.
func TestWorkerStopReleasesInterruptedAttemptWithoutConsumingBudget(t *testing.T) {
	for _, attempts := range []uint32{1, 2} {
		t.Run(map[uint32]string{1: "final-attempt", 2: "retryable"}[attempts], func(t *testing.T) {
			policy := jobs.DefaultPolicy("default")
			policy.Attempts = attempts
			entered := make(chan struct{})
			handler := func(ctx context.Context, _ payload) error { close(entered); <-ctx.Done(); return ctx.Err() }
			f := newWorkerFixture(t, policy, handler)
			var output bytes.Buffer
			f = replaceLoggedWorker(t, f, handler, slog.New(slog.NewJSONHandler(&output, nil)), true)
			recorder, err := observability.New(observability.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := recorder.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			id := f.enqueue(t)
			done := make(chan error, 1)
			go func() { done <- f.worker.Run(observability.WithContext(t.Context(), recorder)) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not start")
			}
			if err := f.worker.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			record := waitRecord(t, f, id, jobs.Waiting)
			if record.Attempts != 0 || record.History[len(record.History)-1].Reason != jobs.WorkerStopped {
				t.Fatal("interrupted attempt consumed its budget", record.Attempts)
			}
			if output.Len() != 0 || recorder.Snapshot().Failures != 0 {
				t.Fatal("ordinary shutdown release reported as failure")
			}
		})
	}
}
