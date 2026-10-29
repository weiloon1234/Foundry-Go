package terminal_lookup

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Run(ctx context.Context, db database.Transactor) (m.WriteRecord, error) {
	_, err := orm.CreateEach(ctx, db, m.WriteRecords.Query(), [][]orm.Assignment[m.WriteRecord]{{orm.TextSet(m.WriteRecords.Name, "one")}})
	if err != nil {
		return m.WriteRecord{}, err
	}
	_, err = orm.FirstOrCreate(ctx, db, m.WriteRecords.Query().Where(orm.TextEq(m.WriteRecords.Name, "one")))
	if err != nil {
		return m.WriteRecord{}, err
	}
	return orm.UpdateOrCreate(ctx, db, m.WriteRecords.Query(), nil, func(context.Context, *database.Tx, m.WriteRecord) ([]orm.Assignment[m.WriteRecord], error) {
		return []orm.Assignment[m.WriteRecord]{orm.TextSet(m.WriteRecords.Name, "two")}, nil
	})
}
