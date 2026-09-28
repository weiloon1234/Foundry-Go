package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongFactory(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = observerqueries.RegisterRecordObserver(r, pool, observerqueries.NewRecordObserver("wrong"), func(foundation.Resolver) (func() observerqueries.PlainHooks, error) {
		return func() observerqueries.PlainHooks { return observerqueries.PlainHooks{} }, nil
	})
}
