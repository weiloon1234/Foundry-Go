package httpkernel

import (
	"context"
	stdhttp "net/http"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func serve(ctx context.Context, handler stdhttp.Handler) error {
	// doc-example:begin http-kernel-1
	serverKey := foundation.NewKey[*foundryhttp.Server]("app.http")
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:8080"
	module := foundryhttp.Module("http", serverKey, config,
		func(resolver foundation.Resolver) (stdhttp.Handler, error) {
			return handler, nil
		},
	)
	app, err := foundry.New().Register(module).Build(ctx)
	if err != nil {
		return err
	}
	return app.Run(ctx, foundation.HTTP)
	// doc-example:end http-kernel-1
}
