package each_writes

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Run(ctx context.Context, db database.Transactor) ([]models.WriteRecord, error) {
	q := models.WriteRecords.Query().Where(orm.ColumnEq(models.WriteRecords.Enabled, false))
	if _, err := orm.UpdateEach(ctx, db, q, 100, func(context.Context, *database.Tx, models.WriteRecord) ([]orm.Assignment[models.WriteRecord], error) {
		return []orm.Assignment[models.WriteRecord]{orm.TextSet(models.WriteRecords.Name, "updated")}, nil
	}); err != nil {
		return nil, err
	}
	return orm.DeleteEach(ctx, db, q, 100)
}
