package invalid

import (
	"context"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func wrong(endpoint h.Endpoint[h.NoPath, h.NoQuery, h.NoBody, h.Stream]) {
	endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.Download, error) {
		return h.Download{}, nil
	})
}
