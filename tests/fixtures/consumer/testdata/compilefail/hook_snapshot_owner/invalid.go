package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = hookqueries.MemberHooks{Deleting: func(context.Context, *database.Tx, mutatorqueries.Member) error { return nil }}
