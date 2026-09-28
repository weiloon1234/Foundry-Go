package http

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type handlerLifetime struct {
	mu       sync.Mutex
	closing  bool
	active   int
	admitted int
	done     chan struct{}
}

func newHandlerLifetime() *handlerLifetime { return &handlerLifetime{done: make(chan struct{})} }

func (o *handlerLifetime) acquire(limit int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.admitted >= limit {
		return false
	}
	o.admitted++
	return true
}

// begin tracks every accepted native invocation, including policy rejections,
// until its completion observers return. Sealed late arrivals run no callbacks.
func (o *handlerLifetime) begin() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing {
		return false
	}
	o.active++
	return true
}
func (o *handlerLifetime) releaseAdmission() { o.mu.Lock(); o.admitted--; o.mu.Unlock() }
func (o *handlerLifetime) release() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active--
	if o.closing && o.active == 0 {
		close(o.done)
	}
}

func (o *handlerLifetime) seal() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing {
		return
	}
	o.closing = true
	if o.active == 0 {
		close(o.done)
	}
}

func (o *handlerLifetime) wrap(handler stdhttp.Handler, logger *slog.Logger, config ServerConfig, observers ...RequestObserver) stdhttp.Handler {
	config = config.Snapshot()
	limit := config.MaxConcurrentRequests
	if limit == 0 {
		limit = DefaultServerConfig().MaxConcurrentRequests
	}
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !o.begin() {
			writeRequestError(w, r, Unavailable, logger)
			return
		}
		admitted := false
		defer func() {
			if admitted {
				o.releaseAdmission()
			}
			o.release()
		}()
		r, err := prepareRequest(r)
		requestLog := logger.With("request_id", RequestID(r.Context()))
		r, observation := beginRequestObservation(r, config.TrustTraceContext)
		if observation == nil && (config.AccessLog || len(observers) > 0) {
			observation = &requestObservation{returned: true}
		}
		var capture *observedResponse
		if observation != nil {
			observation.started = time.Now()
			r = r.WithContext(context.WithValue(r.Context(), requestObservationKey{}, observation))
			capture = &observedResponse{native: w}
			w = responseCapabilities(capture)
		}
		defer func() {
			// Completion hooks are owned work; a slow callback cannot release
			// dependencies while it still uses them.
			if observation != nil {
				observation.complete(r.Context(), Method(r.Method), capture, requestLog, config.AccessLog, observers)
			}
		}()
		r = withRequestLogger(r, requestLog)
		w.Header().Set(RequestIDHeader, string(RequestID(r.Context())))
		if err != nil {
			writeRequestError(w, r, err, requestLog)
			return
		}
		if (observability.FromContext(r.Context()).Gate().Admit() != nil && !config.permitsMaintenanceRead(r)) || !o.acquire(limit) {
			if observation != nil {
				observation.outcome = observability.Rejected
			}
			writeRequestError(w, r, Unavailable, requestLog)
			return
		}
		admitted = true
		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()
		if observation != nil {
			defer func() { observation.contextErr = ctx.Err() }()
		}
		r = r.WithContext(ctx)
		if r.ContentLength > config.MaxBodyBytes {
			writeRequestError(w, r, PayloadTooLarge, requestLog)
			return
		}
		if r.Body != nil {
			r.Body = stdhttp.MaxBytesReader(w, r.Body, config.MaxBodyBytes)
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
			requestLog.ErrorContext(r.Context(), "HTTP handler failed", slog.Any("error", err))
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

func writeRequestError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error, logger *slog.Logger) {
	if writeErr := WriteError(w, r, err); writeErr != nil {
		logger.ErrorContext(r.Context(), "HTTP error response failed", slog.Any("error", writeErr))
	}
}
