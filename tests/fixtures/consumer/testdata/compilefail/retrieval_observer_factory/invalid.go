package invalid

import (
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongFactory(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = retrievalqueries.RegisterMemberRetrievalObserver(r, pool, retrievalqueries.NewMemberRetrievalObserver("wrong"), func(foundation.Resolver) (func() retrievalqueries.GroupRetrievalHooks, error) {
		return func() retrievalqueries.GroupRetrievalHooks { return retrievalqueries.GroupRetrievalHooks{} }, nil
	})
}
