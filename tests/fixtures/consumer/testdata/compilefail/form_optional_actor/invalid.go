package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.AuthorizeOptionalActor[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Account, requestflow.Reply](requestflow.GuestEndpoint(nil, auth.Guard[requestflow.Account]{}), func(context.Context, requestflow.Account, requestflow.Request) error { return nil })
