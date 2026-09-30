package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

const assetParameter = "asset"

type assetRoutePath struct{ Value AssetPath }

// AssetMount declares one named GET/HEAD subtree. Register contributes it to
// NewRouter; URL encodes an AssetPath through the same prefix declaration.
type AssetMount struct {
	assets *Assets
	route  Route[assetRoutePath]
	prefix string
}

func (a *Assets) Mount(id RouteID, prefix string) AssetMount {
	normalized, err := assetPrefix(prefix)
	route := DefineRoute(RouteSpec{ID: id, Method: GET, Access: Public}, DefinePath(normalized+"/{"+assetParameter+"...}", Param(assetParameter, StringPath[AssetPath](), func(p *assetRoutePath) *AssetPath { return &p.Value })))
	if err != nil {
		route.err = err
	}
	return AssetMount{assets: a, route: route, prefix: normalized}
}
func assetPrefix(prefix string) (string, error) {
	if prefix == "/" {
		return "", nil
	}
	if !strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
		return "", fault.New(fault.Invalid, "asset prefix requires an absolute path without a trailing slash")
	}
	if _, err := StaticPath(prefix).validate(); err != nil {
		return "", err
	}
	return prefix, nil
}
func (m AssetMount) Validate() error {
	if m.assets == nil || m.assets.done == nil {
		return fault.New(fault.Invalid, "asset mount requires constructed assets")
	}
	return m.route.Validate()
}
func (m AssetMount) WithMiddleware(middleware ...Middleware) AssetMount {
	m.route = m.route.WithMiddleware(middleware...)
	return m
}
func (m AssetMount) URL(name AssetPath) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	plain, trailing, err := assetPathParts(string(name))
	if err != nil {
		return "", err
	}
	location, err := m.route.URL(assetRoutePath{Value: AssetPath(plain)})
	if err != nil {
		return "", err
	}
	if trailing && plain != "" {
		location += "/"
	}
	return location, nil
}
func (m AssetMount) Register() RouteRegistration {
	if err := m.Validate(); err != nil {
		return RouteRegistration{err: err}
	}
	segments, _ := m.route.validate()
	info := m.route.info(segments, true)
	metadata := m.assets.description(m.prefix)
	info.Assets = &metadata
	// Static paths allow a final directory slash. All other structural path
	// validation shares the ordinary path rules; application binders are unchanged.
	return RouteRegistration{info: info, middlewares: slices.Clone(m.route.middlewares), pattern: string(GET) + " " + nativePath(segments), handler: stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// A more specific SPA owns its subtree: what it declines (a missing
		// asset, an excluded path) is not looked up in this outer mount.
		spa := mountSPA(r, m.prefix)
		if spa != nil && len(spa.prefix) > len(m.prefix) {
			if !spa.serve(w, r) {
				writeRoutingError(w, r, NotFound)
			}
			return
		}
		if err := m.assets.config.Limits.checkRange(r.Header.Values("Range")); err != nil {
			writeRoutingError(w, r, err)
			return
		}
		selection, err := m.assets.selectAsset(r.Context(), r.PathValue(assetParameter), strings.HasSuffix(r.URL.Path, "/"))
		if err != nil {
			if spa != nil && len(spa.prefix) == len(m.prefix) && isMissingAsset(err) && spa.serve(w, r) {
				return
			}
			writeRoutingError(w, r, err)
			return
		}
		m.assets.serveSelection(w, r, selection, m.assets.config.CacheControl)
	})}
}

type assetSelection struct {
	name       string
	redirect   int
	varyAccept bool
}

func (a *Assets) selectAsset(ctx context.Context, raw string, directoryURL bool) (assetSelection, error) {
	name, _, err := assetPathParts(raw)
	if err != nil {
		return assetSelection{}, BadRequest.WithCause(err)
	}
	if !a.config.allowed(name) {
		return assetSelection{}, NotFound
	}
	lookup := name
	if lookup == "" {
		lookup = "."
	}
	info, err := a.stat(ctx, lookup)
	if err != nil {
		return assetSelection{}, err
	}
	selected := assetSelection{name: name}
	if info.mode.IsDir() {
		if a.config.Index == "" {
			return selected, NotFound
		}
		selected.name = path.Join(name, string(a.config.Index))
		index, err := a.stat(ctx, selected.name)
		if err != nil {
			return selected, err
		}
		if !index.mode.IsRegular() {
			return selected, NotFound
		}
		if !directoryURL {
			selected.redirect = 1
		}
	} else {
		if !info.mode.IsRegular() {
			return selected, NotFound
		}
		if directoryURL {
			selected.redirect = -1
		}
	}
	return selected, nil
}
func (a *Assets) serveSelection(w stdhttp.ResponseWriter, r *stdhttp.Request, selection assetSelection, cache HeaderValue) {
	if selection.redirect != 0 {
		location := r.URL.Path
		if selection.redirect > 0 {
			location += "/"
		} else {
			location = strings.TrimSuffix(location, "/")
		}
		// Force origin-form output even for unusual raw requests; never inherit a
		// Host or Scheme from the request for a directory redirect.
		location = "/" + strings.TrimLeft(location, "/")
		target := (&url.URL{Path: location, RawQuery: r.URL.RawQuery}).RequestURI()
		owned := callback.Isolated("HTTP asset redirect", func() error {
			clearResponseRepresentation(w.Header())
			if cache != "" {
				w.Header().Set("Cache-Control", string(cache))
			}
			stdhttp.Redirect(w, r, target, stdhttp.StatusPermanentRedirect)
			return nil
		})
		if owned != nil {
			logRouteFailure(r, "HTTP asset redirect failed", owned)
			panic(stdhttp.ErrAbortHandler)
		}
		return
	}
	response := DownloadResponse(a.config.media(selection.name))
	limits := DefaultEndpointLimits()
	limits.Files = a.config.Limits
	prepared, err := response.prepare(r.Context(), a.Download(AssetPath(selection.name)), limits)
	defer cleanupEndpointResource(r, "asset response", prepared.cleanup())
	if err != nil {
		writeRoutingError(w, r, err)
		return
	}
	prepared.file.cacheControl = cache
	prepared.file.varyAccept = selection.varyAccept
	if err := response.write(w, r, prepared); err != nil {
		logRouteFailure(r, "HTTP asset response failed", err)
		panic(stdhttp.ErrAbortHandler)
	}
}

func isMissingAsset(err error) bool {
	if err == nil {
		return false
	}
	var missing bool
	owned := callback.Isolated("HTTP SPA asset error classification", func() error {
		missing = errorgraph.Is(err, NotFound)
		return nil
	})
	return owned == nil && missing
}
