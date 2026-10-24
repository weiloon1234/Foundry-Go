package invalid

import (
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterObserver(r, pool, orm.NewObserver[observerqueries.Record]("wrong"), func(foundation.Resolver) (func() orm.Hooks[observerqueries.Plain], error) {
		return func() orm.Hooks[observerqueries.Plain] { return orm.Hooks[observerqueries.Plain]{} }, nil
	})
}
