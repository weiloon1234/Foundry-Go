package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	n "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var _ = context.Background
var _ *database.DB
var _ *foundation.Registrar
var _ h.Member
var _ n.Member

func invalid(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[h.KeyedMember]("read"), func(foundation.Resolver) (func() orm.RetrievalHooks[h.Member], error) { return nil, nil })
}
