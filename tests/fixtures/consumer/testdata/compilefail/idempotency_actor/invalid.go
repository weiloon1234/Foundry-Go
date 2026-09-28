package invalid

import (
	"context"
	fixture "foundry.test/consumer/idempotenthttp"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

func bad(endpoint http.AuthenticatedIdempotentEndpoint[fixture.Path, http.NoQuery, fixture.Submission, fixture.Actor, fixture.Receipt]) {
	endpoint.Handle(func(context.Context, fixture.Actor, fixture.Request) (idempotency.Scope, error) {
		return idempotency.Scope{}, nil
	}, func(context.Context, *database.Tx, fixture.Workspace, fixture.Request) (fixture.Receipt, error) {
		return fixture.Receipt{}, nil
	})
}
