package invalid

import (
	"context"
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var endpoint modelbinding.AuthenticatedEndpoint[h.NoPath, h.NoQuery, h.NoBody, models.User, models.Order, h.NoContent]
var _ = endpoint.Handle(func(context.Context, models.User, modelbinding.Input[h.NoPath, h.NoQuery, h.NoBody, models.User]) (h.NoContent, error) {
	return h.NoContent{}, nil
})
