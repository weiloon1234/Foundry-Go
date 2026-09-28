package invalid

import (
	"context"
	"foundry.test/consumer/requestflow"
	"github.com/weiloon1234/Foundry-Go/auth"
)

var _ = requestflow.AccountEndpoint(nil, auth.Guard[requestflow.Account]{}).WithAuthorization(func(context.Context, string, requestflow.Request) error { return nil })
