package invalid

import (
	"context"
	fixture "foundry.test/consumer/idempotenthttp"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

func bad(store *idempotency.Store) {
	fixture.Endpoint().Idempotent(store, idempotency.Definition{ID: "test", Version: 1}).Handle(func(context.Context, fixture.Request) (idempotency.Scope, error) { return idempotency.Scope{}, nil }, func(context.Context, *database.DB, fixture.Request) (fixture.Receipt, error) {
		return fixture.Receipt{}, nil
	})
}

var _ http.NoBody
