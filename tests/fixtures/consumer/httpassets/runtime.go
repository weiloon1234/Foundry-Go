package httpassets

import (
	"context"
	stdhttp "net/http"
	"net/http/httputil"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin portal-runtime-dto

//foundry:dto
type PortalRuntime struct {
	SiteName string `json:"site_name"`
}

// doc-example:end portal-runtime-dto

// doc-example:begin portal-runtime
func Runtime(siteName string) foundryhttp.SPARuntime {
	config := foundryhttp.DefaultSPARuntimeConfig()
	return foundryhttp.DefineSPARuntime(PortalRuntimeJSON(),
		func(ctx context.Context, request *stdhttp.Request) (PortalRuntime, error) {
			if err := ctx.Err(); err != nil {
				return PortalRuntime{}, err
			}
			return PortalRuntime{SiteName: siteName}, nil
		}, config)
}

// doc-example:end portal-runtime

// doc-example:begin configured-spa
func DeclareSPA(builder *application.Builder, source foundryhttp.AssetSource, runtimeData foundryhttp.SPARuntime) *application.Builder {
	spa := foundryhttp.DefaultSPAConfig()
	spa.Prefix, spa.Exclude = "/admin", []string{"/admin/api"}
	spa.Runtime = runtimeData
	return builder.Register(foundryhttp.AssetsModule("web.admin", AssetsKey, foundryhttp.DefaultAssetsConfig(source))).
		SPA("portal.admin", AssetsKey, spa)
}

// doc-example:end configured-spa

// doc-example:begin runtime-proxy
func DevelopmentProxy(proxy *httputil.ReverseProxy, runtimeData foundryhttp.SPARuntime, navigation func(*stdhttp.Request) bool) (stdhttp.Handler, error) {
	return runtimeData.WrapProxy(proxy, navigation)
}

// doc-example:end runtime-proxy
