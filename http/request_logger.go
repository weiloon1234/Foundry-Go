package http

import (
	"context"
	"log/slog"
	stdhttp "net/http"
)

type requestLoggerKey struct{}

// Kernel-owned transport adapters share the injected logger and correlation
// fields. A standalone Router does not implicitly mutate/use slog.Default.
func withRequestLogger(r *stdhttp.Request, logger *slog.Logger) *stdhttp.Request {
	return r.WithContext(context.WithValue(r.Context(), requestLoggerKey{}, logger))
}

// Callers pass framework faults or already-scrubbed callback failures. Panic
// payloads and unclassified user input are never formatted by this helper.
func logRouteFailure(r *stdhttp.Request, message string, err error) {
	logger, _ := r.Context().Value(requestLoggerKey{}).(*slog.Logger)
	if logger == nil {
		return
	}
	info, _ := MatchedRoute(r.Context())
	logger.ErrorContext(r.Context(), message, slog.String("route_id", string(info.ID)), slog.Any("error", err))
}
