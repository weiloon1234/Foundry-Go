package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ = httpendpoints.Update.Handle(func(context.Context, foundryhttp.Input[httpkernel.UserPath, httpquery.NearbyInput, httpdto.OrderResponse]) (httpdto.UserResponse, error) {
	return httpdto.UserResponse{}, nil
})
