package invalid

import (
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongOwner(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = retrievalqueries.RegisterMemberRetrievalObserver(r, pool, retrievalqueries.NewGroupRetrievalObserver("wrong"), func(foundation.Resolver) (func() retrievalqueries.MemberRetrievalHooks, error) {
		return func() retrievalqueries.MemberRetrievalHooks { return retrievalqueries.MemberRetrievalHooks{} }, nil
	})
}
