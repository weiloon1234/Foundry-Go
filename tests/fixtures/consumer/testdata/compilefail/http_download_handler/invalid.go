package invalid

import (
	"context"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func wrong(endpoint h.Endpoint[h.NoPath, h.NoQuery, h.NoBody, h.Download]) {
	endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (string, error) { return "file", nil })
}
