package http

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Scope is an immutable literal path and route-name prefix. Access remains
// explicit on each route. Paths containing parameters belong in the typed Path
// declaration so their bindings stay attached to their concrete parameter type.
type Scope struct {
	prefix      string
	name        RouteID
	err         error
	middlewares []Middleware
}

// DefineScope creates a prefix such as /api/v1 and api.v1. Empty path/name
// prefixes are valid independently. A nonempty prefix has no trailing slash.
func DefineScope(prefix string, name RouteID) Scope {
	scope := Scope{prefix: prefix, name: name}
	if name != "" && !identifier.Semantic(string(name)) {
		scope.err = fault.New(fault.Invalid, "route scope requires a semantic name prefix")
		return scope
	}
	if prefix == "" {
		return scope
	}
	segments, err := httppath.Parse(prefix)
	if err != nil {
		scope.err = err
		return scope
	}
	if strings.HasSuffix(prefix, "/") {
		scope.err = fault.New(fault.Invalid, "route scope prefix cannot end in a slash")
	}
	for _, segment := range segments {
		if segment.Name != "" {
			scope.err = fault.New(fault.Invalid, "route scope prefixes must be literal paths")
		}
	}
	return scope
}

func scopedName(prefix, name RouteID) RouteID {
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return prefix + "." + name
}

// Within nests this scope under its parent without modifying either descriptor.
func (s Scope) Within(parent Scope) Scope {
	if s.err != nil {
		return s
	}
	if parent.err != nil {
		return parent
	}
	nested := DefineScope(parent.prefix+s.prefix, scopedName(parent.name, s.name))
	if nested.err == nil {
		nested.middlewares, nested.err = appendMiddlewares(parent.middlewares, s.middlewares)
	}
	return nested
}

// Within returns a route whose path and name include the scope. Store and reuse
// this returned descriptor so named URLs and registration share the full prefix.
func (r Route[P]) Within(scope Scope) Route[P] {
	if r.err != nil {
		return r
	}
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if scope.err != nil {
		r.err = scope.err
		return r
	}
	r.spec.ID = scopedName(scope.name, r.spec.ID)
	r.path.pattern = scope.prefix + r.path.pattern
	r.middlewares, r.err = appendMiddlewares(scope.middlewares, r.middlewares)
	return r
}
