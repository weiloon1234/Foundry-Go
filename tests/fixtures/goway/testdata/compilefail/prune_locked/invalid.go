package invalid

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = func() { _, _ = orm.PruneBatch(context.Background(), nil, h.Members.Query().ForUpdate(), 1, false) }
