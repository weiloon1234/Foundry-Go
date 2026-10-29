package terminal_create_or_first

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Run(ctx context.Context, db database.Transactor) (m.WriteRecord, error) {
	return orm.CreateOrFirst(ctx, db, m.WriteRecords.Query().Where(orm.TextEq(m.WriteRecords.Name, "one")))
}
