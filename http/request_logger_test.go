package http

import (
	"context"
	"log/slog"
	stdhttp "net/http"
)

// Kernel-owned transport adapters share the injected logger and correlation
// fields. A standalone Router does not implicitly mutate/use slog.Default.
func withRequestLogger(r *stdhttp.Request, logger *slog.Logger) *stdhttp.Request {
	scope := &requestScope{}
	scope.logger.Store(logger)
	return r.WithContext(context.WithValue(r.Context(), requestScopeKey{}, scope))
}
