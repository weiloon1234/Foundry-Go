package logging

import (
	"context"
	"log/slog"
	"slices"
)

var severityLevels = [...]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}

// severityHandler routes each record to one of four identical JSON encoders so
// a severity-aware destination receives the record level without shared
// mutable state between concurrent Handle calls.
type severityHandler struct {
	handlers [len(severityLevels)]slog.Handler
}

func newSeverityHandler(sink *Sink, options Options) slog.Handler {
	var handler severityHandler
	for i, level := range severityLevels {
		handler.handlers[i] = newJSONHandler(severityWriter{sink: sink, level: level}, options)
	}
	return handler
}

func severityIndex(level slog.Level) int {
	switch {
	case level >= slog.LevelError:
		return 3
	case level >= slog.LevelWarn:
		return 2
	case level >= slog.LevelInfo:
		return 1
	default:
		return 0
	}
}

func (h severityHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handlers[0].Enabled(ctx, level)
}
func (h severityHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.handlers[severityIndex(record.Level)].Handle(ctx, record)
}
func (h severityHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	for i, child := range h.handlers {
		h.handlers[i] = child.WithAttrs(slices.Clone(attrs))
	}
	return h
}
func (h severityHandler) WithGroup(name string) slog.Handler {
	for i, child := range h.handlers {
		h.handlers[i] = child.WithGroup(name)
	}
	return h
}

type severityWriter struct {
	sink  *Sink
	level slog.Level
}

func (w severityWriter) Write(data []byte) (int, error) { return w.sink.writeRecord(data, w.level) }
