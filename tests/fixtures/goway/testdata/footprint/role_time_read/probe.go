package role_time_read

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"time"
)

type Model struct {
	ID        int64
	At        time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
type Fields struct {
	orm.Table[Model, int64]
	ID        orm.Primary[Model, int64]
	At        orm.Time[Model, time.Time]
	CreatedAt orm.CreatedAt[Model, time.Time]
	UpdatedAt orm.UpdatedAt[Model, time.Time]
}

var Models = orm.Define("role_times", func(b orm.Builder[Model], m *Model) Fields {
	t := orm.TableOf(b, &m.ID)
	return Fields{t, orm.PrimaryOf(b, t.Key()), orm.TimeOf(b, &m.At), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

func Read(at time.Time) orm.Query[Model] {
	return Models.Query().Where(orm.TimeGt(Models.At, at), orm.TimeLte(Models.CreatedAt, at), orm.TimeGte(Models.UpdatedAt, at)).OrderBy(orm.TimeAsc(Models.CreatedAt), orm.TimeDesc(Models.UpdatedAt))
}
