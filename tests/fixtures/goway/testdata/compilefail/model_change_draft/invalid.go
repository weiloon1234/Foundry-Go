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

var _, _ = orm.Compare(h.Members.Table, value.Set(h.Member{}), value.Set(h.Member{}), m.Members.Name.Set("wrong"))
