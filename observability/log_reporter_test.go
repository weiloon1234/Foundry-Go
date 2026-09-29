package observability_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type steppedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *steppedClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *steppedClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func failedReport(name observability.Name, status int, code fault.Code) observability.ErrorReport {
	return observability.ErrorReport{
		Entry:      observability.Entry{Operation: observability.Operation{Kind: observability.HTTP, Name: name}, Result: observability.Result{Outcome: observability.Failed, Status: status}, RequestID: "request-1"},
		Diagnostic: fault.Diagnostic{Types: []string{"*fault.Error"}, Faults: []fault.Note{{Code: code, Message: "safe framework message"}}},
	}
}

func logRecords(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func TestLogReporterSuppressesDuplicatesAndFilters(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	clock := &steppedClock{now: time.Unix(1000, 0)}
	config := observability.DefaultLogReporterConfig()
	config.Clock = clock
	config.MaxFingerprints = 2
	config.DontReportFaults = []fault.Code{fault.Missing}
	config.DontReportStatuses = []int{503}
	reporter, err := observability.LogReporter(logger, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for range 3 {
		if err := reporter(ctx, failedReport("request", 500, fault.Internal)); err != nil {
			t.Fatal(err)
		}
	}
	_ = reporter(ctx, failedReport("request", 500, fault.Missing))
	_ = reporter(ctx, failedReport("request", 503, fault.Internal))
	if records := logRecords(t, &output); len(records) != 1 || records[0]["msg"] != "operation failed" || records[0]["level"] != "ERROR" || records[0]["request_id"] != "request-1" || records[0]["suppressed"] != nil {
		t.Fatal("duplicates or filtered reports were logged", records)
	}
	clock.advance(time.Minute)
	_ = reporter(ctx, failedReport("request", 500, fault.Internal))
	records := logRecords(t, &output)
	if len(records) != 2 || records[1]["suppressed"] != float64(2) || records[0]["fingerprint"] != records[1]["fingerprint"] {
		t.Fatal("window expiry did not report suppressed duplicates", records)
	}
	// The bound evicts the least recently reported fingerprint.
	_ = reporter(ctx, failedReport("other.one", 500, fault.Internal))
	_ = reporter(ctx, failedReport("other.two", 500, fault.Internal))
	_ = reporter(ctx, failedReport("request", 500, fault.Internal))
	if records := logRecords(t, &output); len(records) != 5 {
		t.Fatal("evicted fingerprint remained suppressed", len(records))
	}
	if strings.Contains(output.String(), "password") {
		t.Fatal("unexpected content")
	}
}

func TestLogReporterValidatesConfiguration(t *testing.T) {
	if _, err := observability.LogReporter(nil, observability.DefaultLogReporterConfig()); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil logger accepted", err)
	}
	for _, mutate := range []func(*observability.LogReporterConfig){
		func(c *observability.LogReporterConfig) { c.MaxFingerprints = 0 },
		func(c *observability.LogReporterConfig) { c.Window = -time.Second },
		func(c *observability.LogReporterConfig) { c.DontReportStatuses = []int{42} },
		func(c *observability.LogReporterConfig) { c.DontReportOutcomes = []observability.Outcome{"unknown"} },
	} {
		config := observability.DefaultLogReporterConfig()
		mutate(&config)
		if _, err := observability.LogReporter(slog.Default(), config); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid reporter configuration accepted", err)
		}
	}
	var output bytes.Buffer
	config := observability.DefaultLogReporterConfig()
	config.Window = 0
	config.DontReportOutcomes = []observability.Outcome{observability.TimedOut}
	reporter, err := observability.LogReporter(slog.New(slog.NewJSONHandler(&output, nil)), config)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_ = reporter(t.Context(), failedReport("request", 500, fault.Internal))
	}
	timedOut := failedReport("request", 0, fault.Timeout)
	timedOut.Entry.Result.Outcome = observability.TimedOut
	_ = reporter(t.Context(), timedOut)
	if records := logRecords(t, &output); len(records) != 2 {
		t.Fatal("zero window must log every report and outcome filters must apply", len(records))
	}
}
