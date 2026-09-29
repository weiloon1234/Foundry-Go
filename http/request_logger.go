package http

import (
	"context"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type requestScopeKey struct{}

// requestScope is the kernel's single per-request context value. It carries the
// injected logger, the optional observation and the kernel's deadline/body
// budget, so the kernel copies the request once instead of once per value.
type requestScope struct {
	base        *slog.Logger
	requestID   attribution.RequestID
	logger      atomic.Pointer[slog.Logger]
	observation *requestObservation
	budget      *requestBudget
	kernel      requestBudget
	// route is the matched route's effective context; see transportErr.
	route atomic.Pointer[routeState]
}

func requestScopeFrom(ctx context.Context) *requestScope {
	scope, _ := ctx.Value(requestScopeKey{}).(*requestScope)
	return scope
}

// log derives the request-correlated logger only when something is logged.
func (s *requestScope) log() *slog.Logger {
	if logger := s.logger.Load(); logger != nil {
		return logger
	}
	if s.base == nil {
		return nil
	}
	s.logger.CompareAndSwap(nil, s.base.With("request_id", s.requestID))
	return s.logger.Load()
}

func requestLogger(ctx context.Context) *slog.Logger {
	if scope := requestScopeFrom(ctx); scope != nil {
		return scope.log()
	}
	return nil
}

// Kernel-owned transport adapters share the injected logger and correlation
// fields. A standalone Router does not implicitly mutate/use slog.Default.
func withRequestLogger(r *stdhttp.Request, logger *slog.Logger) *stdhttp.Request {
	scope := &requestScope{}
	scope.logger.Store(logger)
	return r.WithContext(context.WithValue(r.Context(), requestScopeKey{}, scope))
}

// Callers pass framework faults or already-scrubbed callback failures. Panic
// payloads and unclassified user input are never formatted by this helper.
func logRouteFailure(r *stdhttp.Request, message string, err error) {
	logger := requestLogger(r.Context())
	if logger == nil {
		return
	}
	info, _ := MatchedRoute(r.Context())
	logger.ErrorContext(r.Context(), message, slog.String("route_id", string(info.ID)), slog.Any("error", err))
}

// reportServerFailure records a redacted diagnostic for every 5xx response: in
// the request log and on the request's observation for error reporters. The
// diagnostic holds type names, framework fault notes/attributes (for example a
// SQLSTATE) and panic frames; it never formats the error or request payloads.
// A request whose client already went away is not reported as a server failure.
// Neither is an admission rejection (see admissionRejection): it is counted as a
// rejected request outcome, and describing and logging each one would amplify
// load exactly while the server is saturated or draining.
// admissionRejection reports a deliberate refusal rather than a failure:
// capacity exhaustion (fault.Overloaded), a closing server (fault.Closed), or the
// bare Unavailable decision used for draining and maintenance. A cause such as
// an expired deadline makes an Unavailable response a reported failure.
// Custom error methods run contained; an incomplete or failed inspection
// keeps the response reported.
func admissionRejection(err error) bool {
	if err == error(Unavailable) {
		return true
	}
	rejected := false
	_ = callback.Invoke("HTTP rejection classification", func() error {
		rejected = errorgraph.Is(err, fault.Overloaded) || errorgraph.Is(err, fault.Closed)
		return nil
	})
	return rejected
}

func reportServerFailure(r *stdhttp.Request, payload ErrorResponse, err error) {
	ctx := r.Context()
	if errors.Is(ctx.Err(), context.Canceled) || payload.Code == Unavailable && admissionRejection(err) {
		return
	}
	diagnostic := errordiag.Describe(err)
	recordRequestDiagnostic(ctx, diagnostic)
	logger := requestLogger(ctx)
	if logger == nil {
		return
	}
	info, _ := MatchedRoute(ctx)
	logger.ErrorContext(ctx, "HTTP request failed", slog.String("route_id", string(info.ID)), slog.Int("status", payload.Status), slog.String("error_code", string(payload.Code)), slog.Any("diagnostic", diagnostic))
}
