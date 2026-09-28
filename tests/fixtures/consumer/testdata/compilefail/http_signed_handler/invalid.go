package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpsigned"
)

var links httpsigned.Links
var _ = links.Preview.Handle(func(context.Context, httpendpoints.UpdateRequest) (httpdto.UserResponse, error) {
	return httpdto.UserResponse{}, nil
})
