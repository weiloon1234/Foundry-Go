package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = hookqueries.MemberHooks{Updated: func(context.Context, *database.Tx, mutatorqueries.MemberChanges) error { return nil }}
