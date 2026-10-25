package invalid

import (
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongFactory(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterRetrievalObserver[retrievalqueries.Member](r, pool, orm.NewRetrievalObserver[retrievalqueries.Member]("wrong"), func(foundation.Resolver) (func() orm.RetrievalHooks[retrievalqueries.Group], error) {
		return func() orm.RetrievalHooks[retrievalqueries.Group] { return orm.RetrievalHooks[retrievalqueries.Group]{} }, nil
	})
}
