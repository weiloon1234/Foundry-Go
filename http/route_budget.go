package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxRouteTimeout bounds a declared per-route request deadline. It is the
// framework's safety ceiling for long uploads, downloads and event streams.
const MaxRouteTimeout = 24 * time.Hour

// MaxRouteBodyBytes bounds a declared per-route request-body ceiling.
const MaxRouteBodyBytes int64 = 1 << 40

// routeBudget is a route's own replacement for the kernel's RequestTimeout and
// MaxBodyBytes. Zero fields inherit the kernel configuration.
type routeBudget struct {
	timeout   time.Duration
	bodyBytes int64
}

// WithTimeout replaces the kernel RequestTimeout for this route. The deadline
// starts when the kernel admitted the request, so time spent in global
// middleware counts. A longer timeout also extends the native connection read
// and write deadlines for this request. It must be positive and at most
// MaxRouteTimeout. Client disconnect and forced shutdown still cancel the request.
func (r Route[P]) WithTimeout(timeout time.Duration) Route[P] {
	if r.err == nil {
		if timeout <= 0 || timeout > MaxRouteTimeout {
			r.err = fault.New(fault.Invalid, "route timeout must be positive and at most MaxRouteTimeout")
		} else {
			r.budget.timeout = timeout
		}
	}
	return r
}

// WithBodyLimit replaces the kernel MaxBodyBytes ceiling for this route only,
// for example to accept large uploads without raising the server-wide default.
// It must be positive and at most MaxRouteBodyBytes. Typed endpoints still apply
// their own payload limits, such as EndpointLimits.Multipart.Bytes.
func (r Route[P]) WithBodyLimit(bytes int64) Route[P] {
	if r.err == nil {
		if bytes <= 0 || bytes > MaxRouteBodyBytes {
			r.err = fault.New(fault.Invalid, "route body limit must be positive and at most MaxRouteBodyBytes")
		} else {
			r.budget.bodyBytes = bytes
		}
	}
	return r
}

// WithTimeout replaces the kernel RequestTimeout for this endpoint's route.
func (e Endpoint[P, Q, B, R]) WithTimeout(timeout time.Duration) Endpoint[P, Q, B, R] {
	e.route = e.route.WithTimeout(timeout)
	return e
}

// WithBodyLimit replaces the kernel MaxBodyBytes ceiling for this endpoint's route.
// Raise the matching EndpointLimits payload budget for the endpoint's body kind too.
func (e Endpoint[P, Q, B, R]) WithBodyLimit(bytes int64) Endpoint[P, Q, B, R] {
	e.route = e.route.WithBodyLimit(bytes)
	return e
}

// requestBudget is the kernel's per-request deadline and body configuration.
// The router applies a matched route's own budget from it after matching.
type requestBudget struct {
	// parent is the request context before the kernel deadline: it carries
	// client disconnect and forced-shutdown cancellation.
	parent      context.Context
	started     time.Time
	timeout     time.Duration
	bodyBytes   int64
	ceiling     int64
	readGrace   time.Duration
	writeGrace  time.Duration
	routed      bool
	routeResult error
}

// contextErr reports the error of the context the handler actually used.
func (b *requestBudget) contextErr(kernel context.Context) error {
	if b != nil && b.routed {
		return b.routeResult
	}
	return kernel.Err()
}

// routeContext applies a declared route timeout. Without the kernel, a route
// timeout can only shorten existing deadlines. Within the kernel, a longer
// timeout replaces the kernel deadline while retaining request values and every
// other cancellation source.
func (b routeBudget) context(ctx context.Context, w stdhttp.ResponseWriter, kernel *requestBudget) (context.Context, func()) {
	if b.timeout == 0 {
		return ctx, func() {}
	}
	if kernel == nil {
		routed, cancel := context.WithTimeout(ctx, b.timeout)
		return routed, cancel
	}
	deadline := kernel.started.Add(b.timeout)
	if b.timeout <= kernel.timeout {
		routed, cancel := context.WithDeadline(ctx, deadline)
		return routed, func() { kernel.routed, kernel.routeResult = true, routed.Err(); cancel() }
	}
	routed, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	// The kernel parent carries client disconnect and forced shutdown. Other
	// cancellation in the current chain still applies; only a deadline, which
	// this declaration replaces, is ignored.
	stopParent := context.AfterFunc(kernel.parent, cancel)
	stopChain := context.AfterFunc(ctx, func() {
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			cancel()
		}
	})
	// Native deadlines are best effort: test recorders and some wrappers do not
	// support them, and the request context remains the owning bound.
	controller := stdhttp.NewResponseController(w)
	_ = controller.SetReadDeadline(deadline.Add(kernel.readGrace))
	_ = controller.SetWriteDeadline(deadline.Add(kernel.writeGrace))
	return routed, func() {
		stopChain()
		stopParent()
		kernel.routed, kernel.routeResult = true, routed.Err()
		cancel()
	}
}

// body applies a declared route body ceiling after matching, before any
// handler reads the body. It reports false after writing a 413 response.
func (b routeBudget) body(w stdhttp.ResponseWriter, r *stdhttp.Request, kernel *requestBudget) bool {
	limit := b.bodyBytes
	installed := int64(0)
	if kernel != nil {
		if limit == 0 {
			limit = kernel.bodyBytes
		}
		installed = kernel.ceiling
	}
	if limit == 0 {
		return true
	}
	if r.ContentLength > limit {
		writeRoutingError(w, r, PayloadTooLarge)
		return false
	}
	if (installed == 0 || limit < installed) && r.Body != nil && r.Body != stdhttp.NoBody {
		r.Body = stdhttp.MaxBytesReader(w, r.Body, limit)
	}
	return true
}

// routeBodyCeiling reports the largest declared route body ceiling reachable
// through a handler assembled from a Router. The kernel admits declared bodies
// up to that ceiling; the router then applies each matched route's own limit.
func routeBodyCeiling(handler stdhttp.Handler) int64 {
	if aware, ok := handler.(interface{ routeBodyCeiling() int64 }); ok {
		return aware.routeBodyCeiling()
	}
	return 0
}

// routedHandler retains a router's body ceiling across ApplyMiddleware.
type routedHandler struct {
	stdhttp.Handler
	ceiling int64
}

func (h routedHandler) routeBodyCeiling() int64 { return h.ceiling }
func (h routedHandler) requiresIdempotentMiddleware() bool {
	return requiresIdempotentMiddleware(h.Handler)
}

// routeState publishes the matched route's effective request context to
// response wrappers that run outside the router, such as compression, ETags
// and browser sessions. completed is set once the route's outcome is being
// delivered or its handler returned.
type routeState struct {
	ctx       context.Context
	completed atomic.Bool
}

// transportParent returns the context that carries only real cancellation:
// client disconnect and forced shutdown (the kernel's parent). Outside the
// kernel it is the request context.
func transportParent(r *stdhttp.Request) context.Context {
	if scope := requestScopeFrom(r.Context()); scope != nil && scope.budget != nil && scope.kernel.parent != nil {
		return scope.kernel.parent
	}
	return r.Context()
}

// transportErr reports the cancellation a response wrapper outside the router
// must honor. Client disconnect and forced shutdown always apply. While a
// matched route runs, its effective context applies instead of the kernel
// RequestTimeout, which a route may have replaced (WithTimeout). Once the
// route completed, only real cancellation applies, so a completed success is
// still delivered after a deadline. Before routing, and outside the kernel,
// the wrapper's request context applies.
func transportErr(r *stdhttp.Request) error {
	scope := requestScopeFrom(r.Context())
	if scope == nil || scope.budget == nil || scope.kernel.parent == nil {
		return r.Context().Err()
	}
	if err := scope.kernel.parent.Err(); err != nil {
		return err
	}
	route := scope.route.Load()
	if route == nil {
		return r.Context().Err()
	}
	if route.completed.Load() {
		return nil
	}
	return route.ctx.Err()
}

// publishRoute records a matched route's effective context in the kernel
// scope. The returned function marks the route completed.
func publishRoute(ctx context.Context) func() {
	scope := requestScopeFrom(ctx)
	if scope == nil || scope.budget == nil {
		return func() {}
	}
	route := &routeState{ctx: ctx}
	scope.route.Store(route)
	return func() { route.completed.Store(true) }
}

// completeRoute marks the matched route completed: its handler succeeded and
// its response is being delivered, so wrappers no longer apply its deadline.
func completeRoute(ctx context.Context) {
	if scope := requestScopeFrom(ctx); scope != nil {
		if route := scope.route.Load(); route != nil {
			route.completed.Store(true)
		}
	}
}

// completedContext detaches work that follows a successful handler from a
// request deadline that already expired. It keeps values and real
// cancellation: client disconnect and forced shutdown (the kernel parent)
// still end it, including a client that already left. The returned function
// releases it. An unexpired context is returned unchanged.
func completedContext(ctx context.Context) (context.Context, func()) {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx, func() {}
	}
	detached, cancel := context.WithCancel(context.WithoutCancel(ctx))
	scope := requestScopeFrom(ctx)
	if scope == nil || scope.budget == nil || scope.kernel.parent == nil {
		return detached, cancel
	}
	// A client that already left ends the work at once; AfterFunc alone would
	// cancel asynchronously.
	if scope.kernel.parent.Err() != nil {
		cancel()
		return detached, cancel
	}
	stop := context.AfterFunc(scope.kernel.parent, cancel)
	return detached, func() { stop(); cancel() }
}
