package http

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// handlerLifetime tracks every accepted native invocation without a shared
// lock. state packs the closing flag (bit 0) with the active count (bits 1+).
// Once closing is set no invocation begins, so the count only decreases and the
// single transition to "closing with none active" closes done exactly once.
type handlerLifetime struct {
	state   atomic.Int64
	closing chan struct{}
	done    chan struct{}
	// proxy lets maintenance admission see the TrustedProxy client address.
	proxy *proxyPolicy
}

func newHandlerLifetime() *handlerLifetime {
	return &handlerLifetime{closing: make(chan struct{}), done: make(chan struct{})}
}

// begin tracks every accepted native invocation, including policy rejections,
// until its completion observers return. Sealed late arrivals run no callbacks.
func (o *handlerLifetime) begin() bool {
	for {
		current := o.state.Load()
		if current&1 != 0 {
			return false
		}
		if o.state.CompareAndSwap(current, current+2) {
			return true
		}
	}
}

func (o *handlerLifetime) release() {
	if o.state.Add(-2) == 1 {
		close(o.done)
	}
}

func (o *handlerLifetime) seal() {
	for {
		current := o.state.Load()
		if current&1 != 0 {
			return
		}
		if o.state.CompareAndSwap(current, current|1) {
			close(o.closing)
			if current == 0 {
				close(o.done)
			}
			return
		}
	}
}

func (o *handlerLifetime) wrap(handler stdhttp.Handler, logger *slog.Logger, config ServerConfig, observers ...RequestObserver) stdhttp.Handler {
	config = config.Snapshot()
	limit := config.MaxConcurrentRequests
	if limit == 0 {
		limit = DefaultServerConfig().MaxConcurrentRequests
	}
	capacity := admission.New(limit)
	wait := admission.Wait(config.RequestTimeout)
	// A router that declares larger per-route body limits raises only the early
	// declared-length ceiling; the router applies each matched route's own limit.
	ceiling := max(config.MaxBodyBytes, routeBodyCeiling(handler))
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !o.begin() {
			writeRequestError(w, r, Unavailable, logger)
			return
		}
		admitted := false
		defer func() {
			if admitted {
				capacity.Release()
			}
			o.release()
		}()
		r, err := prepareRequest(r)
		scope := &requestScope{base: logger, requestID: RequestID(r.Context())}
		ctx, observation := beginRequestObservation(r, config.TrustTraceContext)
		if observation == nil && (config.AccessLog || len(observers) > 0) {
			observation = &requestObservation{returned: true}
		}
		var capture *observedResponse
		if observation != nil {
			observation.started = time.Now()
			for _, observer := range observers {
				if _, ok := observer.(SecurityRequestObserver); ok {
					ip := PeerIP(r)
					if o.proxy != nil {
						ip = o.proxy.clientIP(r)
					}
					path, truncated := boundedSecurityPath(r.URL.EscapedPath())
					observation.security = &SecurityRequestEvent{ClientIP: ip, Path: path, PathTruncated: truncated}
					break
				}
			}
			scope.observation = observation
			capture = &observedResponse{native: w}
			w = responseCapabilities(capture)
		}
		// One request copy carries the logger, observation and budget.
		r = r.WithContext(context.WithValue(ctx, requestScopeKey{}, scope))
		defer func() {
			// Completion hooks are owned work; a slow callback cannot release
			// dependencies while it still uses them.
			if observation != nil {
				observation.complete(r.Context(), Method(r.Method), capture, scope, config.AccessLog, observers)
			}
		}()
		w.Header().Set(RequestIDHeader, string(scope.requestID))
		if err != nil {
			writeRequestError(w, r, err, scope.log())
			return
		}
		if decision := admitMaintenance(r, config, o.proxy); !decision.admitted {
			if observation != nil {
				observation.outcome = observability.Rejected
			}
			decision.reject(w, r, scope.log())
			return
		}
		// Capacity waits briefly in arrival order instead of failing a burst
		// immediately. Exhaustion, cancellation and shutdown return 503.
		if err := capacity.Acquire(r.Context(), wait, o.closing); err != nil {
			if observation != nil {
				observation.outcome = observability.Rejected
			}
			writeRequestError(w, r, capacityError(err), scope.log())
			return
		}
		admitted = true
		parent := r.Context()
		scope.kernel = requestBudget{
			parent: parent, started: time.Now(), timeout: config.RequestTimeout,
			bodyBytes: config.MaxBodyBytes, ceiling: ceiling,
			readGrace:  max(config.ReadTimeout-config.RequestTimeout, 0),
			writeGrace: max(config.WriteTimeout-config.RequestTimeout, 0),
		}
		scope.budget = &scope.kernel
		deadline, cancel := context.WithTimeout(parent, config.RequestTimeout)
		defer cancel()
		if observation != nil {
			defer func() { observation.contextErr = scope.budget.contextErr(deadline) }()
		}
		r = r.WithContext(deadline)
		if r.ContentLength > ceiling {
			writeRequestError(w, r, PayloadTooLarge, scope.log())
			return
		}
		if r.Body != nil {
			r.Body = stdhttp.MaxBytesReader(w, r.Body, ceiling)
			defer r.Body.Close()
		}
		var aborted bool
		if observation != nil {
			observation.returned = false
		}
		if err := callback.Invoke("HTTP handler", func() error {
			defer func() {
				if value := recover(); value != nil {
					if value == stdhttp.ErrAbortHandler {
						aborted = true
						return
					}
					panic(value)
				}
			}()
			handler.ServeHTTP(w, r)
			return nil
		}); err != nil {
			if observation != nil {
				observation.outcome = observability.OutcomeFor(err)
			}
			diagnostic := errordiag.Describe(err)
			recordRequestDiagnostic(r.Context(), diagnostic)
			scope.log().ErrorContext(r.Context(), "HTTP handler failed", slog.Any("diagnostic", diagnostic))
			// Raw handler interoperability retains net/http's connection-abort
			// semantics without leaking a panic payload or appending an error to
			// an already-started response. Typed response recovery is separate.
			panic(stdhttp.ErrAbortHandler)
		}
		if aborted {
			if observation != nil {
				observation.outcome = observability.Failed
			}
			panic(stdhttp.ErrAbortHandler)
		}
		if observation != nil {
			observation.returned = true
		}
	})
}

// capacityError keeps every failed capacity wait a 503. Only an exhausted wait
// (fault.Overloaded) carries a retry hint; a closing server or a request that
// ended while queued does not.
func capacityError(err error) error {
	return Unavailable.WithCause(err)
}

func writeRequestError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error, logger *slog.Logger) {
	if writeErr := WriteError(w, r, err); writeErr != nil && logger != nil {
		logger.ErrorContext(r.Context(), "HTTP error response failed", slog.Any("error", writeErr))
	}
}
