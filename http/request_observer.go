package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/observability"
	"log/slog"
	"slices"
	"time"
)

// RequestEvent is a metadata-only completion snapshot. It deliberately excludes
// raw paths/queries, headers, bodies, credentials and error payloads. An empty
// Route means no declared route matched. Hijacked records the HTTP handoff only.
type RequestEvent struct {
	RequestID attribution.RequestID
	Method    Method
	Route     RouteID
	Result    observability.Result
	Duration  time.Duration
	Bytes     int64
	Hijacked  bool
}

// RequestObserver runs once after handling (including rejection/abort), before
// request ownership is released. It must return; its context may be cancelled.
// Panics and Goexit are isolated. Use middleware for request/response behavior.
type RequestObserver interface {
	ObserveRequest(context.Context, RequestEvent)
}
type RequestObserverFunc func(context.Context, RequestEvent)

func (f RequestObserverFunc) ObserveRequest(ctx context.Context, event RequestEvent) { f(ctx, event) }

type serverOptions struct {
	observers []RequestObserver
	// proxy resolves client address and scheme for kernel-level admission.
	proxy *proxyPolicy
}
type ServerOption func(*serverOptions) error

func WithRequestObserver(observer RequestObserver) ServerOption {
	return func(o *serverOptions) error {
		if credential.IsNil(observer) {
			return fault.New(fault.Invalid, "request observer is nil")
		}
		if len(o.observers) >= 16 {
			return fault.New(fault.Invalid, "too many request observers")
		}
		o.observers = append(o.observers, observer)
		return nil
	}
}
func configureServer(options []ServerOption) (serverOptions, error) {
	var o serverOptions
	for _, option := range slices.Clone(options) {
		if option == nil {
			return o, fault.New(fault.Invalid, "nil HTTP server option")
		}
		if err := option(&o); err != nil {
			return o, err
		}
	}
	return o, nil
}

// recordRequestDiagnostic keeps the first server-failure diagnostic of a request.
func recordRequestDiagnostic(ctx context.Context, diagnostic fault.Diagnostic) {
	if scope := requestScopeFrom(ctx); scope != nil && scope.observation != nil {
		observation := scope.observation
		observation.diagnosticMu.Lock()
		if observation.diagnostic.IsZero() {
			observation.diagnostic = diagnostic.Clone()
		}
		observation.diagnosticMu.Unlock()
	}
}

func (o *requestObservation) complete(ctx context.Context, method Method, response *observedResponse, scope *requestScope, access bool, observers []RequestObserver) {
	result := o.result(response)
	var route RouteID
	if matched := o.route.Load(); matched != nil {
		route = matched.info.ID
	}
	o.diagnosticMu.Lock()
	diagnostic := o.diagnostic
	o.diagnosticMu.Unlock()
	if o.span != nil {
		var declared attribution.Route
		if matched := o.route.Load(); matched != nil {
			declared = attribution.Route{Method: string(matched.info.Method), Name: string(matched.info.ID)}
		}
		o.span.EndWithRoute(result, diagnostic, declared)
	}
	// Native requests can carry extension methods; a fixed label avoids retaining
	// attacker-controlled method text in automatic diagnostic records.
	if !method.valid() {
		method = "OTHER"
	}
	event := RequestEvent{RequestID: RequestID(ctx), Method: method, Route: route, Result: result, Duration: time.Since(o.started), Bytes: response.bytes, Hijacked: response.hijacked}
	if access {
		scope.log().InfoContext(ctx, "HTTP request completed", slog.String("method", string(event.Method)), slog.String("route", string(event.Route)), slog.Int("status", result.Status), slog.String("outcome", string(result.Outcome)), slog.Duration("duration", event.Duration), slog.Int64("bytes", event.Bytes), slog.Bool("hijacked", event.Hijacked))
	}
	for _, observer := range observers {
		if security, ok := observer.(SecurityRequestObserver); ok && o.security != nil {
			owned := *o.security
			owned.Request = event
			if err := callback.Isolated("HTTP security observer", func() error { security.ObserveSecurityRequest(ctx, owned); return nil }); err != nil {
				scope.log().ErrorContext(ctx, "HTTP security observer failed", slog.Any("error", err))
			}
		}
		if err := callback.Isolated("HTTP request observer", func() error { observer.ObserveRequest(ctx, event); return nil }); err != nil {
			scope.log().ErrorContext(ctx, "HTTP request observer failed", slog.Any("error", err))
		}
	}
}
