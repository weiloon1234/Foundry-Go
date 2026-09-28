package invalid

import (
	"context"
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, tx *database.Tx, r *audit.Recorder) {
	_, _ = auditqueries.Approved.Record(ctx, tx, r, "wrong payload")
}
