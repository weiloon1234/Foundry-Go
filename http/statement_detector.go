package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"log/slog"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// StatementDetectorMiddlewareID identifies the opt-in per-request N+1 diagnostic.
const StatementDetectorMiddlewareID MiddlewareID = "foundry.database.statement_shapes"

// StatementDetector begins a scope per request and logs redacted shape violations
// after the handler returns. Install the same detector on the database pools.
// Example: builder.Use(http.StatementDetector(detector,logger)).
func StatementDetector(detector *database.StatementDetector, logger *slog.Logger) Middleware {
	return DefineMiddleware(StatementDetectorMiddlewareID, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if detector == nil || logger == nil {
			return nil, safeerror.NewFault(fault.Invalid, "statement detector middleware requires detector and logger")
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			ctx, scope := detector.Begin(r.Context())
			defer func() {
				report := scope.Report()
				for _, v := range report.Violations {
					logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelWarn, "repeated database statement shape", slog.String("shape", v.Shape), slog.Int("count", v.Count), slog.Int("threshold", v.Threshold), slog.Any("call_sites", v.CallSites), slog.Bool("call_sites_truncated", v.CallSiteOverflow))
				}
				if report.Overflow || report.CallSiteOverflow {
					logger.WarnContext(context.WithoutCancel(ctx), "database statement shape observation incomplete", slog.Bool("shape_capacity_exceeded", report.Overflow), slog.Bool("call_sites_truncated", report.CallSiteOverflow))
				}
			}()
			next.ServeHTTP(w, r.WithContext(ctx))
		}), nil
	})
}
