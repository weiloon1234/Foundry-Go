package rows_each

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Stream(ctx context.Context, db database.Executor, yield func(int64) error) error {
	return rows.EachRead(ctx, db, rows.Read(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents)), yield)
}
