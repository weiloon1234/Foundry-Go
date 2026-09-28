package invalid

import (
	"context"
	"foundry.test/consumer/hookqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = hookqueries.MemberHooks{Creating: func(context.Context, *database.Tx, *mutatorqueries.MemberDraft) error { return nil }}
