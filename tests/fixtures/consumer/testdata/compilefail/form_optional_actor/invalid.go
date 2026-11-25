package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.AuthorizeOptionalActor[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Account, requestflow.Reply](requestflow.GuestEndpoint(http.GuardBinding[requestflow.Account]{}), func(context.Context, requestflow.Account, requestflow.Request) error { return nil })
