package http

import (
	"cmp"
	"context"
	stdhttp "net/http"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type spaFallback struct {
	assets *Assets
	config SPAConfig
	prefix string
	info   RouteInfo
	state  *matchedRoute
}

// WithSPA returns an independent router view sharing the immutable native route
// table. Only a native unmatched 404 can reach this fallback. The original router
// is unchanged; no endpoint error response is intercepted or rewritten.
func (r *Router) WithSPA(id RouteID, assets *Assets, config SPAConfig) (*Router, error) {
	if r == nil || r.mux == nil {
		return nil, fault.New(fault.Invalid, "SPA requires a router")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	mount := assets.Mount(id, config.Prefix)
	if err := mount.Validate(); err != nil {
		return nil, err
	}
	for _, prior := range r.spas {
		if prior.prefix == mount.prefix {
			return nil, fault.New(fault.Duplicate, "SPA prefix already configured")
		}
	}
	if !assets.config.allowed(string(config.Index)) {
		return nil, fault.New(fault.Invalid, "SPA entry is hidden by asset policy")
	}
	for _, route := range r.routes {
		if route.ID == id {
			return nil, fault.New(fault.Duplicate, "SPA route ID is already registered")
		}
	}
	registration := mount.Register()
	info := registration.info.clone()
	info.Assets.Fallback = true
	info.Assets.Index = config.Index
	info.Assets.Excluded = slices.Clone(config.Exclude)
	result := *r
	result.routes = append(slices.Clone(r.routes), info.clone())
	slices.SortFunc(result.routes, func(a, b RouteInfo) int { return cmp.Compare(a.ID, b.ID) })
	result.spas = append(slices.Clone(r.spas), &spaFallback{assets: assets, config: config.snapshot(), prefix: mount.prefix, info: info, state: &matchedRoute{info: info}})
	slices.SortFunc(result.spas, func(a, b *spaFallback) int {
		if depth := cmp.Compare(len(b.prefix), len(a.prefix)); depth != 0 {
			return depth
		}
		return cmp.Compare(a.prefix, b.prefix)
	})
	return &result, nil
}
func (s *spaFallback) serve(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
	if s == nil || (r.Method != stdhttp.MethodGet && r.Method != stdhttp.MethodHead) || !matchesAssetPrefix(r.URL.Path, s.prefix) {
		return false
	}
	for _, prefix := range s.config.Exclude {
		if matchesAssetPrefix(r.URL.Path, prefix) {
			return false
		}
	}
	if scope := requestScopeFrom(r.Context()); scope != nil && scope.observation != nil {
		scope.observation.route.Store(s.state)
	}
	matched := r.WithContext(context.WithValue(r.Context(), matchedRouteKey{}, s.state))
	if err := s.assets.config.Limits.checkRange(r.Header.Values("Range")); err != nil {
		writeRoutingError(w, matched, err)
		return true
	}
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, s.prefix), "/")
	var selection assetSelection
	var err error
	if name == "" {
		selection, err = s.entry(r.Context())
		if err == nil && s.prefix != "" && !strings.HasSuffix(r.URL.Path, "/") {
			selection.redirect = 1
		}
	} else {
		selection, err = s.assets.selectAsset(r.Context(), name, strings.HasSuffix(r.URL.Path, "/"))
		if isMissingAsset(err) && s.assets.config.allowed(strings.TrimSuffix(name, "/")) && htmlNavigation(r.Header.Values("Accept"), name) {
			selection, err = s.entry(r.Context())
			selection.varyAccept = true
		}
	}
	if isMissingAsset(err) {
		return false
	}
	if err != nil {
		writeRoutingError(w, matched, err)
		return true
	}
	s.assets.serveSelection(w, matched, selection, s.config.CacheControl)
	return true
}

func (s *spaFallback) entry(ctx context.Context) (assetSelection, error) {
	info, err := s.assets.stat(ctx, string(s.config.Index))
	if err != nil {
		return assetSelection{}, err
	}
	if !info.mode.IsRegular() {
		return assetSelection{}, InternalError.WithCause(fault.New(fault.Invalid, "SPA entry must be a regular file"))
	}
	return assetSelection{name: string(s.config.Index)}, nil
}

// Select the most specific prefix once. A missing asset or excluded API path
// in that application must not fall through to an unrelated outer SPA.
func (r *Router) serveSPA(w stdhttp.ResponseWriter, request *stdhttp.Request) bool {
	for _, fallback := range r.spas {
		if matchesAssetPrefix(request.URL.Path, fallback.prefix) {
			return fallback.serve(w, request)
		}
	}
	return false
}
