package invalid

import (
	"context"
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, db *database.DB, r *audit.Recorder) {
	_, _ = auditqueries.Approved.Record(ctx, db, r, auditqueries.Approval{})
}
