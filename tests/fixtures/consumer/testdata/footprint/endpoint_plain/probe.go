package endpoint_plain

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func Route() foundryhttp.RouteRegistration {
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(
			foundryhttp.RouteSpec{
				ID:     "probe.ping",
				Method: foundryhttp.POST,
				Access: foundryhttp.Public,
			},
			foundryhttp.StaticPath("/ping"),
		),
		foundryhttp.EmptyQuery(),
		foundryhttp.EmptyBody(),
		foundryhttp.EmptyResponse(204),
	)
	return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
}
