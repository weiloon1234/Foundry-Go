package tooling

import (
	"context"
	"fmt"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func Records() (*factory.Factory[models.WriteRecord, []orm.Assignment[models.WriteRecord]], error) {
	return factory.Define(models.WriteRecords.Table, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[models.WriteRecord], error) {
		return []orm.Assignment[models.WriteRecord]{models.WriteRecords.Name.Set(fmt.Sprintf("record-%d", n))}, nil
	})
}
func UserPlan(ctx context.Context, executor database.Executor) (orm.Plan, error) {
	return orm.Explain(ctx, executor, models.Users.Query().Limit(10))
}
func LockedUserPlan(ctx context.Context, tx *database.Tx) (orm.Plan, error) {
	return orm.ExplainAnalyzeLocked(ctx, tx, models.Users.Query().ForUpdate())
}
