package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

type noticeKind int

const (
	noticeWrite noticeKind = iota
	noticeDropped
	noticeRotation
	noticePrune
	noticeKinds
)

var noticeMessages = [noticeKinds]string{
	noticeWrite:    "log destination write failed; failed records fall back to stderr when bounded",
	noticeDropped:  "async log queue is full; newest records are dropped and counted",
	noticeRotation: "log rotation failed; records continue in the current file and rotation is retried with backoff",
	noticePrune:    "log retention cleanup failed; records are still written and cleanup is retried with backoff",
}

// sinkEvents owns one sink's counters and one-time degradation notices. Its
// methods are nil-safe so private rotation tests can omit accounting.
type sinkEvents struct {
	driver                                SinkDriver
	notices                               io.Writer
	records, failures, fallbacks, dropped atomic.Uint64
	rotationFailures, pruneFailures       atomic.Uint64
	noticed                               [noticeKinds]atomic.Bool
}

func newSinkEvents(driver SinkDriver, notices io.Writer) *sinkEvents {
	return &sinkEvents{driver: driver, notices: notices}
}

func (e *sinkEvents) snapshot() SinkStats {
	if e == nil {
		return SinkStats{}
	}
	return SinkStats{Driver: e.driver, Records: e.records.Load(), Failures: e.failures.Load(), Fallbacks: e.fallbacks.Load(), Dropped: e.dropped.Load(), RotationFailures: e.rotationFailures.Load(), PruneFailures: e.pruneFailures.Load()}
}

func (e *sinkEvents) rotationFailed(err error) {
	if e != nil {
		e.rotationFailures.Add(1)
		e.notice(noticeRotation, err)
	}
}

func (e *sinkEvents) pruneFailed(err error) {
	if e != nil {
		e.pruneFailures.Add(1)
		e.notice(noticePrune, err)
	}
}

// notice writes one fixed-text record per kind directly to the notice writer,
// never through the failing sink. It contains the redacted error diagnostic
// and the operating-system error name, never arbitrary error text or payloads.
func (e *sinkEvents) notice(kind noticeKind, err error) {
	if e == nil || e.notices == nil || e.noticed[kind].Swap(true) {
		return
	}
	attrs := []slog.Attr{slog.String("sink", string(e.driver))}
	if err != nil {
		attrs = append(attrs, slog.Any("diagnostic", errordiag.Describe(err)))
		var errno syscall.Errno
		if errors.As(err, &errno) {
			attrs = append(attrs, slog.String("errno", errno.Error()))
		}
	}
	record := slog.NewRecord(time.Now(), slog.LevelError, "Foundry "+noticeMessages[kind], 0)
	record.AddAttrs(attrs...)
	_ = newJSONHandler(e.notices, Options{}).Handle(context.Background(), record)
}

// retryDelay is the bounded exponential backoff shared by rotation and
// retention retries: one minute, doubling up to one hour.
func retryDelay(failures int) time.Duration {
	delay := time.Minute
	for i := 1; i < failures && delay < time.Hour; i++ {
		delay *= 2
	}
	return min(delay, time.Hour)
}
