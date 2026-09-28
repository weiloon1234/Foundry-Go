package tooling

import (
	"context"
	"fmt"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func Records() (*factory.Factory[models.WriteRecord, models.WriteRecordDraft], error) {
	return factory.New[models.WriteRecord](func(_ context.Context, n factory.Sequence) (models.WriteRecordDraft, error) {
		return models.WriteRecordDraft{}.SetName(fmt.Sprintf("record-%d", n)), nil
	})
}
func UserPlan(ctx context.Context, executor database.Executor) (query.Plan, error) {
	return models.QueryUsers().Limit(10).Explain(ctx, executor)
}
func LockedUserPlan(ctx context.Context, tx *database.Tx) (query.Plan, error) {
	return models.QueryUsers().ForUpdate().ExplainAnalyze(ctx, tx)
}
