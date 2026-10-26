package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, executor database.Executor) {
	_, _ = models.QueryUsers().ForUpdate().ExplainAnalyze(ctx, executor)
}
