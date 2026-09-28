package invalid

import (
	"context"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = softqueries.MemberHooks{Restoring: func(context.Context, *database.Tx, softqueries.Membership) error { return nil }}
