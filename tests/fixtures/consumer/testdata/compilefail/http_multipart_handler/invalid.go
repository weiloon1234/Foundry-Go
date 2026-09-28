package invalid

import (
	"context"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type First struct{ File h.UploadedFile }
type Second struct{ File h.UploadedFile }

func wrong(endpoint h.Endpoint[h.NoPath, h.NoQuery, First, h.NoContent]) {
	endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, Second]) (h.NoContent, error) {
		return h.NoContent{}, nil
	})
}
