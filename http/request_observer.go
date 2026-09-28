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

type serverOptions struct{ observers []RequestObserver }
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

type requestObservationKey struct{}

func recordMatchedRoute(ctx context.Context, id RouteID) {
	if observation, ok := ctx.Value(requestObservationKey{}).(*requestObservation); ok {
		observation.routeMu.Lock()
		observation.route = id
		observation.routeMu.Unlock()
	}
}
func (o *requestObservation) complete(ctx context.Context, method Method, response *observedResponse, logger *slog.Logger, access bool, observers []RequestObserver) {
	result := o.result(response)
	if o.span != nil {
		o.span.End(result)
	}
	o.routeMu.Lock()
	route := o.route
	o.routeMu.Unlock()
	// Native requests can carry extension methods; a fixed label avoids retaining
	// attacker-controlled method text in automatic diagnostic records.
	if !method.valid() {
		method = "OTHER"
	}
	event := RequestEvent{RequestID: RequestID(ctx), Method: method, Route: route, Result: result, Duration: time.Since(o.started), Bytes: response.bytes, Hijacked: response.hijacked}
	if access {
		logger.InfoContext(ctx, "HTTP request completed", slog.String("method", string(event.Method)), slog.String("route", string(event.Route)), slog.Int("status", result.Status), slog.String("outcome", string(result.Outcome)), slog.Duration("duration", event.Duration), slog.Int64("bytes", event.Bytes), slog.Bool("hijacked", event.Hijacked))
	}
	for _, observer := range observers {
		if err := callback.Isolated("HTTP request observer", func() error { observer.ObserveRequest(ctx, event); return nil }); err != nil {
			logger.ErrorContext(ctx, "HTTP request observer failed", slog.Any("error", err))
		}
	}
}
