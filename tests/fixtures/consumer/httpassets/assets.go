// Package httpassets verifies framework-owned static assets and SPA assembly.
package httpassets

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log/slog"
	stdhttp "net/http"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

//go:embed public/*
var public embed.FS

var AssetsKey = foundation.NewKey[*foundryhttp.Assets]("consumer.web.assets")
var ServerKey = foundation.NewKey[*foundryhttp.Server]("consumer.web.server")

func EmbeddedSource() (foundryhttp.AssetSource, error) {
	files, err := fs.Sub(public, "public")
	if err != nil {
		return foundryhttp.AssetSource{}, err
	}
	return foundryhttp.FilesystemAssets(files), nil
}

// Build declares source, routes and services; Foundry opens the directory at
// boot, serves and drains HTTP, then closes asset resources. No os.Root is
// opened or closed by this consumer. The same source can be embedded or local.
func Build(ctx context.Context, source foundryhttp.AssetSource) (*foundation.App, error) {
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	return foundry.New(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(
		foundryhttp.AssetsModule("assets", AssetsKey, foundryhttp.DefaultAssetsConfig(source)),
		foundryhttp.Module("http", ServerKey, config, func(resolver foundation.Resolver) (stdhttp.Handler, error) {
			assets, err := foundation.Resolve(resolver, AssetsKey)
			if err != nil {
				return nil, err
			}
			mount := assets.Mount("web.assets", "/assets")
			health := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "api.health", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/api/health")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
			router, err := foundryhttp.NewRouter(mount.Register(), health.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
				return foundryhttp.NoContent{}, nil
			}))
			if err != nil {
				return nil, err
			}
			spa := foundryhttp.DefaultSPAConfig()
			spa.Exclude = []string{"/api", "/assets"}
			return router.WithSPA("web.spa", assets, spa)
		}),
	).Build(ctx)
}
