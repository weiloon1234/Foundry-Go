package observability

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// LogReporterConfig controls the built-in structured-log error reporter.
// Window suppresses repeated reports sharing one fingerprint (operation,
// outcome, status and redacted diagnostic); zero logs every report. The first
// report after a window closes carries the suppressed count. DontReport*
// filters skip reports whose diagnostic contains one of the fault codes, whose
// HTTP status or whose outcome is listed.
type LogReporterConfig struct {
	Level              slog.Level
	Window             time.Duration
	MaxFingerprints    int
	DontReportFaults   []fault.Code
	DontReportStatuses []int
	DontReportOutcomes []Outcome
	// Clock measures suppression windows; nil uses the system clock.
	Clock clock.Clock
}

func DefaultLogReporterConfig() LogReporterConfig {
	return LogReporterConfig{Level: slog.LevelError, Window: time.Minute, MaxFingerprints: 256}
}

func (c LogReporterConfig) Validate() error {
	if c.Level < slog.LevelDebug || c.Level > slog.LevelError || c.Window < 0 || c.Window > 24*time.Hour || c.MaxFingerprints < 1 || c.MaxFingerprints > 4096 {
		return fault.New(fault.Invalid, "invalid error log reporter bounds")
	}
	if len(c.DontReportFaults) > 64 || len(c.DontReportStatuses) > 64 || len(c.DontReportOutcomes) > 8 {
		return fault.New(fault.Invalid, "too many error log reporter filters")
	}
	for _, status := range c.DontReportStatuses {
		if status < 100 || status > 599 {
			return fault.New(fault.Invalid, "invalid error log reporter status filter")
		}
	}
	for _, outcome := range c.DontReportOutcomes {
		if _, err := (Result{Outcome: outcome}).normalize(Job); err != nil {
			return err
		}
	}
	return nil
}

type fingerprint [sha256.Size]byte

type suppression struct {
	key        fingerprint
	emitted    time.Time
	suppressed uint64
}

type logReporter struct {
	logger  *slog.Logger
	config  LogReporterConfig
	mu      sync.Mutex
	entries map[fingerprint]*list.Element
	order   *list.List
}

// LogReporter logs each accepted error report once as a structured record with
// the redacted diagnostic and correlation IDs. It never logs error text or
// payloads. Duplicate suppression retains at most MaxFingerprints entries; the
// least recently reported fingerprint is forgotten first.
func LogReporter(logger *slog.Logger, config LogReporterConfig) (Reporter, error) {
	if logger == nil {
		return nil, fault.New(fault.Invalid, "error log reporter requires a logger")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	config.DontReportFaults = slices.Clone(config.DontReportFaults)
	config.DontReportStatuses = slices.Clone(config.DontReportStatuses)
	config.DontReportOutcomes = slices.Clone(config.DontReportOutcomes)
	if config.Clock == nil {
		config.Clock = clock.System{}
	}
	reporter := &logReporter{logger: logger, config: config, entries: make(map[fingerprint]*list.Element), order: list.New()}
	return reporter.report, nil
}

func (r *logReporter) skipped(report ErrorReport) bool {
	if slices.Contains(r.config.DontReportStatuses, report.Entry.Result.Status) || slices.Contains(r.config.DontReportOutcomes, report.Entry.Result.Outcome) {
		return true
	}
	for _, note := range report.Diagnostic.Faults {
		if slices.Contains(r.config.DontReportFaults, note.Code) {
			return true
		}
	}
	return false
}

// admit returns whether to log now and how many duplicates were suppressed
// since the previous record for this fingerprint.
func (r *logReporter) admit(key fingerprint, now time.Time) (bool, uint64) {
	if r.config.Window == 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if element := r.entries[key]; element != nil {
		entry := element.Value.(*suppression)
		// A wall clock stepping backwards ends the window instead of extending it.
		if now.Sub(entry.emitted) < r.config.Window && !now.Before(entry.emitted) {
			entry.suppressed++
			return false, 0
		}
		suppressed := entry.suppressed
		entry.emitted, entry.suppressed = now, 0
		r.order.MoveToFront(element)
		return true, suppressed
	}
	if r.order.Len() >= r.config.MaxFingerprints {
		oldest := r.order.Back()
		delete(r.entries, oldest.Value.(*suppression).key)
		r.order.Remove(oldest)
	}
	r.entries[key] = r.order.PushFront(&suppression{key: key, emitted: now})
	return true, 0
}

func (r *logReporter) report(ctx context.Context, report ErrorReport) error {
	if r.skipped(report) {
		return nil
	}
	key := reportFingerprint(report)
	emit, suppressed := r.admit(key, r.config.Clock.Now())
	if !emit {
		return nil
	}
	entry := report.Entry
	attrs := []slog.Attr{
		slog.Group("operation", slog.String("kind", string(entry.Operation.Kind)), slog.String("name", string(entry.Operation.Name))),
		slog.String("outcome", string(entry.Result.Outcome)),
		slog.Duration("duration", entry.Duration),
		slog.String("trace_id", entry.TraceID.String()),
		slog.String("span_id", entry.SpanID.String()),
		slog.String("fingerprint", hex.EncodeToString(key[:8])),
	}
	if entry.Result.Status != 0 {
		attrs = append(attrs, slog.Int("status", entry.Result.Status))
	}
	if entry.RequestID != "" {
		attrs = append(attrs, slog.String("request_id", string(entry.RequestID)))
	}
	if !report.Diagnostic.IsZero() {
		attrs = append(attrs, slog.Any("diagnostic", report.Diagnostic))
	}
	if suppressed > 0 {
		attrs = append(attrs, slog.Uint64("suppressed", suppressed))
	}
	r.logger.LogAttrs(ctx, r.config.Level, "operation failed", attrs...)
	return nil
}

// reportFingerprint hashes the stable failure identity: never IDs, times or
// durations, so repeated occurrences of one failure share a fingerprint.
func reportFingerprint(report ErrorReport) fingerprint {
	hash := sha256.New()
	write := func(parts ...string) {
		for _, part := range parts {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(part)))
			hash.Write(size[:])
			hash.Write([]byte(part))
		}
	}
	entry := report.Entry
	write(string(entry.Operation.Kind), string(entry.Operation.Name), string(entry.Result.Outcome), strconv.Itoa(entry.Result.Status))
	diagnostic := report.Diagnostic
	write("types", strconv.Itoa(len(diagnostic.Types)))
	write(diagnostic.Types...)
	write("faults", strconv.Itoa(len(diagnostic.Faults)))
	for _, note := range diagnostic.Faults {
		write(string(note.Code), note.Message)
	}
	write("attributes", strconv.Itoa(len(diagnostic.Attributes)))
	for _, attribute := range diagnostic.Attributes {
		write(attribute.Key, attribute.Value)
	}
	write("frames", strconv.Itoa(len(diagnostic.Frames)))
	for _, frame := range diagnostic.Frames {
		write(frame.Function, frame.File, strconv.Itoa(frame.Line))
	}
	write(strconv.FormatBool(diagnostic.Truncated))
	var result fingerprint
	hash.Sum(result[:0])
	return result
}
