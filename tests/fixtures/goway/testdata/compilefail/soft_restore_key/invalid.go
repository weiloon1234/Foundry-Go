package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = context.Background
var _ *database.DB
var _ foundation.Resolver
var _ value.Optional[h.Member]
var _ m.Member
var _ orm.Hooks[h.Member]

func invalid(db *database.DB) {
	_, _ = orm.Restore(context.Background(), db, h.KeyedMembers.Table, model.ID[m.Member]{})
}
