package http

import (
	stdhttp "net/http"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// MiddlewareID is the semantic identity of one middleware declaration. It is
// distinct from route IDs and retained in route inspection metadata.
type MiddlewareID string

// Middleware is an immutable native HTTP wrapper declaration. Its factory runs
// once during assembly and must not start I/O or goroutines. Request handling
// remains synchronous; wrappers must not retain a writer after ServeHTTP ends.
type Middleware struct {
	id         MiddlewareID
	idempotent bool
	construct  func(stdhttp.Handler) (stdhttp.Handler, error)
}

// DefineMiddleware accepts a native net/http wrapper and its typed identity.
// The constructor may return a configuration error. It is not invoked until
// ApplyMiddleware or NewRouter assembles the handler chain.
func DefineMiddleware(id MiddlewareID, construct func(stdhttp.Handler) (stdhttp.Handler, error)) Middleware {
	return Middleware{id: id, construct: construct}
}

func (m Middleware) ID() MiddlewareID { return m.id }

func (m Middleware) Validate() error {
	if !identifier.Semantic(string(m.id)) || m.construct == nil {
		return fault.New(fault.Invalid, "HTTP middleware requires a semantic ID and constructor")
	}
	return nil
}

const maxMiddlewares = 128

func validateMiddlewares(middlewares []Middleware) error {
	if len(middlewares) > maxMiddlewares {
		return fault.New(fault.Invalid, "HTTP middleware chain exceeds its declaration bound")
	}
	seen := make(map[MiddlewareID]bool, len(middlewares))
	for _, item := range middlewares {
		if err := item.Validate(); err != nil {
			return err
		}
		if seen[item.id] {
			return fault.New(fault.Duplicate, "HTTP middleware ID is repeated in one chain: "+string(item.id))
		}
		seen[item.id] = true
	}
	return nil
}

func appendMiddlewares(outer, inner []Middleware) ([]Middleware, error) {
	if len(outer) > maxMiddlewares-len(inner) {
		return nil, fault.New(fault.Invalid, "HTTP middleware chain exceeds its declaration bound")
	}
	owned := append(slices.Clone(outer), inner...)
	if err := validateMiddlewares(owned); err != nil {
		return nil, err
	}
	return owned, nil
}

func middlewareIDs(middlewares []Middleware) []MiddlewareID {
	if len(middlewares) == 0 {
		return nil
	}
	ids := make([]MiddlewareID, len(middlewares))
	for i, item := range middlewares {
		ids[i] = item.id
	}
	return ids
}

// ApplyMiddleware constructs a chain in declaration order: the first item is
// outermost on the request and last on the response. Constructors run inside-out
// once; no request is started here. Apply this around a Router for global policy,
// including redirects, missing routes and method errors. Route inspection lists
// only route/scope middleware; it does not infer wrappers outside the router.
// The original native ResponseWriter is preserved unless a supplied wrapper
// explicitly replaces it. Optional writer capabilities remain its responsibility.
func ApplyMiddleware(handler stdhttp.Handler, middlewares ...Middleware) (stdhttp.Handler, error) {
	if nilHTTPHandler(handler) {
		return nil, fault.New(fault.Invalid, "HTTP middleware requires a handler")
	}
	owned, err := appendMiddlewares(nil, middlewares)
	if err != nil {
		return nil, err
	}
	protected := requiresIdempotentMiddleware(handler)
	if protected {
		if err := validateIdempotentMiddlewares(owned); err != nil {
			return nil, err
		}
	}
	current := handler
	for i := len(owned) - 1; i >= 0; i-- {
		item := owned[i]
		var wrapped stdhttp.Handler
		err := callback.Isolated("HTTP middleware constructor", func() error {
			var err error
			wrapped, err = item.construct(current)
			return err
		})
		if err != nil {
			return nil, fault.Wrap(fault.Invalid, "HTTP middleware construction failed: "+string(item.id), err)
		}
		if nilHTTPHandler(wrapped) {
			return nil, fault.New(fault.Invalid, "HTTP middleware returned an empty handler: "+string(item.id))
		}
		current = wrapped
	}
	if protected {
		current = idempotentHTTPHandler{current}
	}
	return current, nil
}

func nilHTTPHandler(handler stdhttp.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}
