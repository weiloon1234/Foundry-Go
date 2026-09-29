package attribution

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	MaxRouteMethodBytes = 16
	MaxRouteNameBytes   = 256
)

// Route identifies the matched server endpoint of the current request by its
// method and declared route name. It is request-local context metadata: Origin
// serialization, queued jobs, events and outbox records do not carry it. Raw
// URLs, query strings and path values are deliberately excluded because they
// can contain credentials or personal data.
type Route struct {
	Method string
	Name   string
}

func (Route) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("attribution route")) }

func (r Route) Validate() error {
	if r.Method == "" || len(r.Method) > MaxRouteMethodBytes || strings.IndexFunc(r.Method, func(c rune) bool { return c < 'A' || c > 'Z' }) >= 0 {
		return fault.New(fault.Invalid, "invalid attribution route method")
	}
	if r.Name == "" || len(r.Name) > MaxRouteNameBytes || !utf8.ValidString(r.Name) ||
		strings.IndexFunc(r.Name, func(c rune) bool { return unicode.IsControl(c) || unicode.IsSpace(c) }) >= 0 {
		return fault.New(fault.Invalid, "invalid attribution route name")
	}
	return nil
}

type routeContextKey struct{}

// WithRoute attaches the matched route for the remainder of this request. It
// grants no authority and does not change the request's Origin.
func WithRoute(ctx context.Context, route Route) (context.Context, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "attribution requires a context")
	}
	if err := route.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, routeContextKey{}, route), nil
}

// RouteFromContext returns the matched route, or false outside a routed request.
func RouteFromContext(ctx context.Context) (Route, bool) {
	if ctx == nil {
		return Route{}, false
	}
	route, ok := ctx.Value(routeContextKey{}).(Route)
	return route, ok
}
