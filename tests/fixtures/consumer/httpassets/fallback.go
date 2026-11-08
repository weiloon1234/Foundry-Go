package httpassets

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// DeclareFallbacks demonstrates root public assets beside a scoped development
// proxy. The caller supplies its proxy and application-rendered navigation page.
// doc-example:begin configured-fallbacks
func DeclareFallbacks(builder *application.Builder, source foundryhttp.AssetSource, proxy, lost stdhttp.Handler) (*application.Builder, error) {
	api, err := foundryhttp.ParsePathPrefix("/api")
	if err != nil {
		return nil, err
	}
	account, err := foundryhttp.ParsePathPrefix("/account")
	if err != nil {
		return nil, err
	}
	bundles, err := foundryhttp.ParsePathPrefix("/account/_assets")
	if err != nil {
		return nil, err
	}
	return builder.Register(foundryhttp.AssetsModule("web.public", AssetsKey, foundryhttp.DefaultAssetsConfig(source))).
		Fallback(func(services application.Services) ([]foundryhttp.Fallback, error) {
			assets, err := application.Resolve(services, AssetsKey)
			if err != nil {
				return nil, err
			}
			publicConfig := foundryhttp.DefaultFallbackConfig()
			publicConfig.Exclude = []foundryhttp.PathPrefix{api, account}
			proxyConfig := foundryhttp.DefaultFallbackConfig()
			proxyConfig.Exclude = []foundryhttp.PathPrefix{bundles}
			return []foundryhttp.Fallback{
				assets.Mount("web.public", "/").WithNavigationFallback(lost).Fallback(publicConfig),
				foundryhttp.DefineFallback("web.account.dev", account, proxy, proxyConfig),
			}, nil
		}), nil
}

// doc-example:end configured-fallbacks

// doc-example:begin router-fallback
func RouterFallback(router *foundryhttp.Router, handler stdhttp.Handler) (*foundryhttp.Router, error) {
	root, err := foundryhttp.ParsePathPrefix("/")
	if err != nil {
		return nil, err
	}
	api, err := foundryhttp.ParsePathPrefix("/api")
	if err != nil {
		return nil, err
	}
	config := foundryhttp.DefaultFallbackConfig()
	config.Exclude = []foundryhttp.PathPrefix{api}
	return router.WithFallbacks(foundryhttp.DefineFallback("pages.missing", root, handler, config))
}

// doc-example:end router-fallback
