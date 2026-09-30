// Package spaportals verifies SPA fallbacks declared through application.New:
// browser portals on distinct prefixes beside a root public asset mount, a more
// specific hashed-bundle mount with immutable caching and typed API routes.
package spaportals

import (
	"context"
	"embed"
	"io/fs"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

//go:embed web
var web embed.FS

var (
	PublicKey   = foundation.NewKey[*foundryhttp.Assets]("consumer.portals.public")
	AdminKey    = foundation.NewKey[*foundryhttp.Assets]("consumer.portals.admin")
	BundlesKey  = foundation.NewKey[*foundryhttp.Assets]("consumer.portals.admin_bundles")
	MerchantKey = foundation.NewKey[*foundryhttp.Assets]("consumer.portals.merchant")
	HomeKey     = foundation.NewKey[*foundryhttp.Assets]("consumer.portals.home")
)

// ImmutableCache is the policy of content-hashed bundles; SPA entries keep
// SPAConfig.CacheControl (no-cache) independently of it.
const ImmutableCache foundryhttp.HeaderValue = "public, max-age=31536000, immutable"

func assets(name foundation.ProviderID, key foundation.Key[*foundryhttp.Assets], dir string, cache foundryhttp.HeaderValue) (foundation.Provider, error) {
	files, err := fs.Sub(web, "web/"+dir)
	if err != nil {
		return nil, err
	}
	config := foundryhttp.DefaultAssetsConfig(foundryhttp.FilesystemAssets(files))
	if cache != "" {
		config.CacheControl = cache
	}
	return foundryhttp.AssetsModule(name, key, config), nil
}

// Build assembles the portals with application.New. With home, a portal at "/"
// sits beside the root public mount: the mount serves its own files and the
// home portal answers the mount's misses.
func Build(ctx context.Context, settings application.Settings, home bool, options ...application.Option) (*application.App, error) {
	builder, err := New(settings, options...)
	if err != nil {
		return nil, err
	}
	return Declare(builder, home).Build(ctx)
}

// New registers the portal assets and ordinary routes, without SPAs.
func New(settings application.Settings, options ...application.Option) (*application.Builder, error) {
	builder := application.New(settings, options...)
	for _, module := range []struct {
		name  foundation.ProviderID
		key   foundation.Key[*foundryhttp.Assets]
		dir   string
		cache foundryhttp.HeaderValue
	}{
		{"portals.public", PublicKey, "public", ""},
		{"portals.admin", AdminKey, "admin", ""},
		{"portals.admin_bundles", BundlesKey, "admin/assets", ImmutableCache},
		{"portals.merchant", MerchantKey, "merchant", ""},
		{"portals.home", HomeKey, "home", ""},
	} {
		provider, err := assets(module.name, module.key, module.dir, module.cache)
		if err != nil {
			return nil, err
		}
		builder.Register(provider)
	}
	return builder.HTTP(Routes), nil
}

// Declare adds the portal SPAs. The admin portal excludes its API namespace.
func Declare(builder *application.Builder, home bool) *application.Builder {
	admin := foundryhttp.DefaultSPAConfig()
	admin.Prefix, admin.Exclude = "/admin", []string{"/admin/api"}
	merchant := foundryhttp.DefaultSPAConfig()
	merchant.Prefix = "/merchant"
	builder.SPA("portals.admin", AdminKey, admin).SPA("portals.merchant", MerchantKey, merchant)
	if home {
		builder.SPA("portals.home", HomeKey, foundryhttp.DefaultSPAConfig())
	}
	return builder
}

// Routes declares the ordinary routes: the root public mount, the admin
// portal's hashed bundles and a typed API route inside the admin prefix.
func Routes(services application.Services) ([]foundryhttp.RouteRegistration, error) {
	public, err := application.Resolve(services, PublicKey)
	if err != nil {
		return nil, err
	}
	bundles, err := application.Resolve(services, BundlesKey)
	if err != nil {
		return nil, err
	}
	session := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "admin.session", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/admin/api/session")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	return []foundryhttp.RouteRegistration{
		public.Mount("portals.public_files", "/").Register(),
		bundles.Mount("portals.admin_bundles", "/admin/assets").Register(),
		session.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			return "admin", nil
		}),
	}, nil
}
