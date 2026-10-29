package role_soft_read

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

type Model struct {
	ID        int64
	DeletedAt value.Nullable[time.Time]
}
type Fields struct {
	orm.Table[Model, int64]
	DeletedAt orm.SoftDelete[Model, time.Time]
	Soft      orm.SoftDeletable[Model]
}

var Models = orm.Define("role_soft", func(b orm.Builder[Model], m *Model) Fields {
	t := orm.TableOf(b, &m.ID)
	d := orm.SoftDeleteOf(b, &m.DeletedAt)
	return Fields{t, d, orm.SoftDeletes(d)}
})

func Read() orm.Query[Model] {
	return Models.Query().WithTrashed().Where(orm.NullableTimeIsNull(Models.DeletedAt))
}
