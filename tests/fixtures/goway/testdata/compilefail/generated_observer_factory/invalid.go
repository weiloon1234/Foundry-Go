package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = context.Background
var _ *database.DB
var _ foundation.Resolver
var _ value.Optional[h.Member]
var _ m.Member
var _ orm.Hooks[h.Member]

func invalid(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	_ = orm.RegisterObserver(r, pool, orm.NewObserver[h.Member]("wrong"), func(foundation.Resolver) (func() orm.Hooks[m.Member], error) {
		return func() orm.Hooks[m.Member] { return orm.Hooks[m.Member]{} }, nil
	})
}
