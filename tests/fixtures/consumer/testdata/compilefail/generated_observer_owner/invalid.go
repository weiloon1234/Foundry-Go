package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterObserver[observerqueries.Record](r, pool, orm.NewObserver[observerqueries.Plain]("wrong"), func(foundation.Resolver) (func() orm.Hooks[observerqueries.Record], error) {
		return func() orm.Hooks[observerqueries.Record] { return orm.Hooks[observerqueries.Record]{} }, nil
	})
}
