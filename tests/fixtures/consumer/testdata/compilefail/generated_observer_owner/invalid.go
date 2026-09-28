package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongOwner(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = observerqueries.RegisterRecordObserver(r, pool, observerqueries.NewPlainObserver("wrong"), func(foundation.Resolver) (func() observerqueries.RecordHooks, error) {
		return func() observerqueries.RecordHooks { return observerqueries.RecordHooks{} }, nil
	})
}
