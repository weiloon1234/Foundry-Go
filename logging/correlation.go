package logging

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

// Correlate adds a reserved correlation group from the context of each log
// call, plus a reserved "context" group holding fields attached with WithAttrs.
// JSON uses it by default. Custom application loggers can wrap their handler
// explicitly. It retains no context between calls and never includes vendor
// state, request bodies, subject identities or arbitrary context values. Pass
// a non-nil handler and keep application fields outside both reserved groups.
func Correlate(handler slog.Handler) slog.Handler { return correlationHandler{handler} }

type correlationHandler struct{ next slog.Handler }

// Enabled delegates level filtering to the underlying handler.
func (h correlationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// WithAttrs returns a correlation handler with the underlying attributes applied.
func (h correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return correlationHandler{h.next.WithAttrs(attrs)}
}

// WithGroup returns a correlation handler with the underlying group applied.
func (h correlationHandler) WithGroup(name string) slog.Handler {
	return correlationHandler{h.next.WithGroup(name)}
}

// Handle adds request, trace, and shared context attributes before forwarding the record.
func (h correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	var fields []slog.Attr
	if request := attribution.FromContext(ctx).Request(); request.ID != "" {
		fields = append(fields, slog.String("request_id", string(request.ID)))
	}
	if trace := tracing.FromContext(ctx); !trace.IsZero() {
		fields = append(fields, slog.String("trace_id", trace.TraceID().String()), slog.String("span_id", trace.SpanID().String()))
	}
	shared := contextAttrs(ctx)
	if len(fields) != 0 || len(shared) != 0 {
		record = record.Clone()
		if len(fields) != 0 {
			record.AddAttrs(slog.Attr{Key: "correlation", Value: slog.GroupValue(fields...)})
		}
		if len(shared) != 0 {
			record.AddAttrs(slog.Attr{Key: "context", Value: slog.GroupValue(shared...)})
		}
	}
	return h.next.Handle(ctx, record)
}
