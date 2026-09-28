package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
)

var _ = httpendpoints.Update.Handle(func(context.Context, httpendpoints.UpdateRequest) (httpdto.OrderResponse, error) {
	return httpdto.OrderResponse{}, nil
})
