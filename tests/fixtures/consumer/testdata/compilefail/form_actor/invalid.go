package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.AuthorizeActor[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Account, requestflow.Reply](requestflow.AccountEndpoint(nil, auth.Guard[requestflow.Account]{}), func(context.Context, string, requestflow.Request) error { return nil })
