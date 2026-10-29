package role_primary_number

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct{ ID int64 }
type Fields struct {
	orm.Table[Model, int64]
	ID orm.NumberPrimary[Model, int64]
}

var Models = orm.Define("role_keys", func(b orm.Builder[Model], m *Model) Fields {
	t := orm.TableOf(b, &m.ID)
	return Fields{t, orm.NumberKey(orm.PrimaryOf(b, t.Key()))}
})

func Read(id int64) orm.Query[Model] {
	return Models.Query().Where(orm.NumberKeyEq(Models.ID, id)).OrderBy(orm.NumberKeyAsc(Models.ID))
}
