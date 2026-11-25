package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.AuthorizeActor[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Account, requestflow.Reply](requestflow.AccountEndpoint(http.GuardBinding[requestflow.Account]{}), func(context.Context, string, requestflow.Request) error { return nil })
