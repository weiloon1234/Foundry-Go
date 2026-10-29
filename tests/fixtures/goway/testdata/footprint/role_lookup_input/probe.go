package role_lookup_input

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct{ ID int64 }
type Fresh int64
type Fields struct {
	orm.Table[Model, int64]
	ID orm.InputKey[Model, Fresh, int64]
}

var Models = orm.Define("role_inputs", func(b orm.Builder[Model], m *Model) Fields {
	t := orm.TableOf(b, &m.ID)
	return Fields{t, orm.InputKeyOf(b, t.Key(), func(i Fresh) (int64, error) { return int64(i), nil })}
})

func Read(id int64) orm.Query[Model] {
	return Models.Query().Where(orm.NumberKeyEq(orm.NumberKey(Models.Key()), id), orm.InputKeyEq(Models.ID, id))
}
