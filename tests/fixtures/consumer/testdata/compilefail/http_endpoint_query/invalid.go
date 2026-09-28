package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ = httpendpoints.Update.Handle(func(context.Context, foundryhttp.Input[httpkernel.UserPath, foundryhttp.NoQuery, httpdto.UpdateUser]) (httpdto.UserResponse, error) {
	return httpdto.UserResponse{}, nil
})
