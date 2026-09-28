package invalid

import (
	"context"
	fixture "foundry.test/consumer/idempotenthttp"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

func bad(store *idempotency.Store) {
	fixture.Endpoint().Idempotent(store, idempotency.Definition{ID: "test", Version: 1}).Handle(func(context.Context, fixture.Request) (idempotency.Scope, error) { return idempotency.Scope{}, nil }, func(context.Context, *database.Tx, fixture.Request) (fixture.Submission, error) {
		return fixture.Submission{}, nil
	})
}
