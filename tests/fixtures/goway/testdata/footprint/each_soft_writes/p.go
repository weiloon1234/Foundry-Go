package each_soft_writes

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

type Record struct {
	ID        int64
	DeletedAt value.Nullable[time.Time]
}
type table struct {
	orm.Table[Record, int64]
	Soft orm.SoftDeletable[Record]
}

var records = orm.Define("soft_records", func(b orm.Builder[Record], m *Record) table {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return table{orm.TableOf(b, &m.ID), orm.SoftDeletes(deleted)}
})

func Run(ctx context.Context, db database.Transactor) ([]Record, error) {
	if _, err := orm.RestoreEach(ctx, db, records.Query(), records.Soft, 100); err != nil {
		return nil, err
	}
	return orm.ForceDeleteEach(ctx, db, records.Query(), records.Soft, 100)
}
