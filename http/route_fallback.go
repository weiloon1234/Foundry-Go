package http

import (
	"cmp"
	stdhttp "net/http"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const fallbackLimit = 64

// PathPrefix is a validated absolute literal URL prefix. Its zero value is invalid.
// Matching includes the prefix itself and descendants at segment boundaries.
type PathPrefix struct{ path string }

// ParsePathPrefix rejects empty, relative, encoded and noncanonical prefixes.
func ParsePathPrefix(text string) (PathPrefix, error) {
	if len(text) > 4096 {
		return PathPrefix{}, safeerror.NewFault(fault.Invalid, "fallback prefix exceeds limit")
	}
	if _, err := assetPrefix(text); err != nil {
		return PathPrefix{}, err
	}
	return PathPrefix{path: text}, nil
}

func (p PathPrefix) String() string     { return p.path }
func (p PathPrefix) normalized() string { return strings.TrimSuffix(p.path, "/") }

// FallbackConfig declares exact methods and declaration-local exclusions.
// GET does not implicitly add HEAD; DefaultFallbackConfig declares both.
type FallbackConfig struct {
	Methods []Method
	Exclude []PathPrefix
}

func DefaultFallbackConfig() FallbackConfig { return FallbackConfig{Methods: []Method{GET, HEAD}} }

func (c FallbackConfig) Validate() error {
	if len(c.Methods) == 0 || len(c.Methods) > fallbackLimit || len(c.Exclude) > fallbackLimit {
		return safeerror.NewFault(fault.Invalid, "fallback requires methods and bounded exclusions")
	}
	methods := make(map[Method]bool, len(c.Methods))
	for _, method := range c.Methods {
		if !method.valid() {
			return safeerror.NewFault(fault.Invalid, "invalid fallback method")
		}
		if methods[method] {
			return safeerror.NewFault(fault.Duplicate, "duplicate fallback method")
		}
		methods[method] = true
	}
	prefixes := make(map[PathPrefix]bool, len(c.Exclude))
	for _, prefix := range c.Exclude {
		if prefix.path == "" {
			return safeerror.NewFault(fault.Invalid, "fallback exclusion requires a constructed prefix")
		}
		if prefixes[prefix] {
			return safeerror.NewFault(fault.Duplicate, "duplicate fallback exclusion")
		}
		prefixes[prefix] = true
	}
	return nil
}
func (c FallbackConfig) snapshot() FallbackConfig {
	c.Methods = slices.Clone(c.Methods)
	c.Exclude = slices.Clone(c.Exclude)
	return c
}

// Fallback is an immutable unmatched-request declaration. Construct it with
// DefineFallback or AssetMount.Fallback; a zero declaration is invalid.
type Fallback struct {
	id      RouteID
	prefix  PathPrefix
	config  FallbackConfig
	handler stdhttp.Handler
	mount   *AssetMount
}

// DefineFallback snapshots configuration and retains the supplied handler.
// The handler receives the original URL, without typed path decoding.
func DefineFallback(id RouteID, prefix PathPrefix, handler stdhttp.Handler, config FallbackConfig) Fallback {
	return Fallback{id: id, prefix: prefix, handler: handler, config: config.snapshot()}
}

// Fallback serves this mount after ordinary routing. It retains its middleware
// and navigation fallback, and never registers a native catch-all.
func (m AssetMount) Fallback(config FallbackConfig) Fallback {
	prefix := m.prefix
	if prefix == "" {
		prefix = "/"
	}
	parsed, _ := ParsePathPrefix(prefix)
	return Fallback{id: RouteIDOf(m.route), prefix: parsed, config: config.snapshot(), mount: &m}
}

func (f Fallback) Validate() error {
	if !identifier.Semantic(string(f.id)) || f.prefix.path == "" {
		return safeerror.NewFault(fault.Invalid, "fallback requires a semantic ID and a constructed prefix")
	}
	if err := f.config.Validate(); err != nil {
		return err
	}
	if f.mount != nil {
		if err := f.mount.Validate(); err != nil {
			return err
		}
		for _, method := range f.config.Methods {
			if method != GET && method != HEAD {
				return safeerror.NewFault(fault.Invalid, "asset fallback supports GET and HEAD only")
			}
		}
	} else if f.handler == nil {
		return safeerror.NewFault(fault.Invalid, "fallback requires a handler")
	}
	return nil
}

// FallbackInfo describes unmatched ownership, without exporting a client operation.
type FallbackInfo struct {
	Prefix  PathPrefix
	Methods []Method
	Exclude []PathPrefix
}

func (i FallbackInfo) clone() FallbackInfo {
	i.Methods = slices.Clone(i.Methods)
	i.Exclude = slices.Clone(i.Exclude)
	return i
}

type routeFallback struct {
	declaration Fallback
	handler     stdhttp.Handler
	state       *matchedRoute
}

// WithFallbacks returns an independent router view. Native matches, redirects
// and method errors take precedence. On a native 404, the most specific owner
// is selected once; its exclusions, method misses and asset misses are final.
// Equal-prefix assets and a SPA compose; equal-prefix raw handlers and SPAs conflict.
func (r *Router) WithFallbacks(fallbacks ...Fallback) (*Router, error) {
	if r == nil || r.mux == nil {
		return nil, safeerror.NewFault(fault.Invalid, "fallback requires a router")
	}
	if len(fallbacks) > fallbackLimit-len(r.fallbacks) {
		return nil, safeerror.NewFault(fault.Invalid, "too many fallback declarations")
	}
	result := *r
	result.routes = slices.Clone(r.routes)
	result.fallbacks = slices.Clone(r.fallbacks)
	for _, f := range fallbacks {
		if err := f.Validate(); err != nil {
			return nil, err
		}
		for _, route := range result.routes {
			if route.ID == f.id {
				return nil, safeerror.NewFault(fault.Duplicate, "fallback route ID is already registered")
			}
		}
		for _, prior := range result.fallbacks {
			if prior.declaration.prefix == f.prefix {
				return nil, safeerror.NewFault(fault.Duplicate, "fallback prefix already configured")
			}
		}
		for _, spa := range r.spas {
			if spa.prefix == f.prefix.normalized() && f.mount == nil {
				return nil, safeerror.NewFault(fault.Conflict, "raw fallback and SPA share a prefix")
			}
		}
		details := &FallbackInfo{Prefix: f.prefix, Methods: slices.Clone(f.config.Methods), Exclude: slices.Clone(f.config.Exclude)}
		info := RouteInfo{ID: f.id, Method: f.config.Methods[0], Path: f.prefix.normalized() + "/{path...}", Access: Public, Raw: true, Fallback: true, FallbackDetails: details}
		handler := f.handler
		if f.mount != nil {
			mount := *f.mount
			segments, _ := routeValidate(mount.route)
			info = routeInfo(mount.route, segments, true)
			info.Method = f.config.Methods[0]
			info.Fallback, info.FallbackDetails = true, details
			metadata := mount.assets.description(mount.prefix)
			metadata.Fallback = true
			info.Assets = &metadata
			var err error
			handler, err = ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
				mount.serve(w, request, fallbackAssetName(request, mount.prefix))
			}), mount.route.middlewares...)
			if err != nil {
				return nil, err
			}
		}
		result.routes = append(result.routes, info.clone())
		result.fallbacks = append(result.fallbacks, &routeFallback{declaration: f, handler: handler, state: &matchedRoute{info: info, budget: routeBudget{timeout: info.Timeout, bodyBytes: info.MaxBodyBytes}}})
	}
	slices.SortFunc(result.routes, func(a, b RouteInfo) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(result.fallbacks, func(a, b *routeFallback) int {
		return cmp.Compare(len(b.declaration.prefix.path), len(a.declaration.prefix.path))
	})
	return &result, nil
}

func (r *Router) serveFallback(w stdhttp.ResponseWriter, request *stdhttp.Request) bool {
	spa := coveringSPA(r.spas, request.URL.Path)
	var selected *routeFallback
	for _, f := range r.fallbacks {
		if matchesAssetPrefix(request.URL.Path, f.declaration.prefix.normalized()) {
			selected = f
			break
		}
	}
	if spa != nil && (selected == nil || len(spa.prefix) > len(selected.declaration.prefix.normalized())) {
		if !spa.accepts(request) {
			return false
		}
		if len(r.fallbacks) != 0 && !validateFallbackAssetRequest(w, request, spa.prefix) {
			return true
		}
		return spa.serve(w, request)
	}
	if selected == nil {
		return false
	}
	f := selected.declaration
	if !slices.Contains(f.config.Methods, Method(request.Method)) {
		return false
	}
	for _, prefix := range f.config.Exclude {
		if matchesAssetPrefix(request.URL.Path, prefix.normalized()) {
			return false
		}
	}
	if f.mount != nil && !validateFallbackAssetRequest(w, request, f.prefix.normalized()) {
		return true
	}
	var kernel *requestBudget
	if scope := requestScopeFrom(request.Context()); scope != nil {
		kernel = scope.budget
	}
	ctx, release := selected.state.budget.context(matchedContext(request.Context(), selected.state), w, kernel)
	defer release()
	defer publishRoute(ctx)()
	request = request.WithContext(ctx)
	if !selected.state.budget.body(w, request, kernel) {
		return true
	}
	selected.handler.ServeHTTP(w, request)
	return true
}

func fallbackAssetName(r *stdhttp.Request, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/")
}

func validateFallbackAssetRequest(w stdhttp.ResponseWriter, r *stdhttp.Request, prefix string) bool {
	escaped := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		writeRoutingError(w, r, BadRequest)
		return false
	}
	if _, _, err := assetPathParts(fallbackAssetName(r, prefix)); err != nil {
		writeRoutingError(w, r, BadRequest.WithCause(err))
		return false
	}
	return true
}
