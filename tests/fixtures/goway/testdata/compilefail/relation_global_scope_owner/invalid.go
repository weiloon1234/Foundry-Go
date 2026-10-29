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
var _ = orm.WithoutRelationGlobalScope(n.Manager, orm.Scope(n.Groups.Table, "tenant", func(context.Context) (orm.Predicate[n.Group], error) { return orm.KeyEq(n.Groups.ID, 1), nil }))
