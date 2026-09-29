package schedule

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

// reportSkipped logs occurrences the scheduler decided not to run. It runs
// outside the scheduler mutex so a slow sink cannot stall admission.
func (s *Scheduler) reportSkipped(ctx context.Context, records []Record) {
	for _, record := range records {
		s.log(ctx, slog.LevelWarn, "schedule occurrence skipped",
			slog.String("schedule", string(record.Invocation.Schedule)),
			slog.String("occurrence_id", record.Invocation.Occurrence.String()),
			slog.Time("intended_at", record.Invocation.IntendedAt),
			slog.String("reason", string(record.Reason)))
	}
}

// reportFinished logs failed and overlap-skipped invocations with a redacted
// diagnostic: type names, framework fault codes and panic frames, never error
// text or payloads. Successful and cancelled invocations are not logged.
func (s *Scheduler) reportFinished(ctx context.Context, invocation Invocation, state State, reason Reason, diagnostic fault.Diagnostic) {
	level, message := slog.LevelError, "schedule invocation failed"
	switch state {
	case Failed:
	case Skipped:
		// A declined When predicate is a deliberate decision, not a problem.
		if reason == Filtered {
			return
		}
		level, message = slog.LevelWarn, "schedule occurrence skipped"
	default:
		return
	}
	attrs := []slog.Attr{slog.String("schedule", string(invocation.Schedule)),
		slog.String("occurrence_id", invocation.Occurrence.String()),
		slog.Time("intended_at", invocation.IntendedAt),
		slog.String("state", string(state)), slog.String("reason", string(reason))}
	if !diagnostic.IsZero() {
		attrs = append(attrs, slog.Any("diagnostic", diagnostic))
	}
	s.log(ctx, level, message, attrs...)
}

func (s *Scheduler) logFailure(ctx context.Context, id ID, message string, err error) {
	s.log(ctx, slog.LevelError, message, slog.String("schedule", string(id)), slog.Any("diagnostic", errordiag.Describe(err)))
}

func (s *Scheduler) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	s.mu.Lock()
	logger := s.logger
	s.mu.Unlock()
	if logger == nil {
		return
	}
	_ = callback.Isolated("log schedule outcome", func() error {
		logger.LogAttrs(context.WithoutCancel(ctx), level, message, attrs...)
		return nil
	})
}
