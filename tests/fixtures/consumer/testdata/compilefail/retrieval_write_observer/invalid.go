package invalid

import (
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongKind(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterRetrievalObserver[retrievalqueries.Member](r, pool, orm.NewObserver[retrievalqueries.Member]("wrong"), func(foundation.Resolver) (func() orm.RetrievalHooks[retrievalqueries.Member], error) {
		return func() orm.RetrievalHooks[retrievalqueries.Member] {
			return orm.RetrievalHooks[retrievalqueries.Member]{}
		}, nil
	})
}
