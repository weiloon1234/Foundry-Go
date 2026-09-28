package invalid

import (
	"context"
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB, r *audit.Recorder, id audit.ModelID[auditqueries.Label]) {
	_, _ = audit.FindModel(ctx, db, r, (auditqueries.Account{}).FoundryReference(), id)
}
