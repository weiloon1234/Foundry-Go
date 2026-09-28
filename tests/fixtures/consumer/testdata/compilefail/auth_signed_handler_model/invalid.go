package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint authenticating.SignedOrderEndpoint
var _ = endpoint.Handle(func(context.Context, models.Order, h.Input[authenticating.OrderPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
	return h.NoContent{}, nil
})
