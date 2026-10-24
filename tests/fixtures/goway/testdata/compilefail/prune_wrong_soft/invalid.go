package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var q orm.Query[int]
var _ = func() { _, _ = orm.ForcePruneBatch(context.Background(), nil, q, h.Members.Soft, 1, false) }
