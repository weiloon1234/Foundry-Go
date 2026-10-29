package role_primary_text

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct{ ID string }
type Fields struct {
	orm.Table[Model, string]
	ID orm.TextPrimary[Model, string]
}

var Models = orm.Define("role_keys", func(b orm.Builder[Model], m *Model) Fields {
	t := orm.TableOf(b, &m.ID)
	return Fields{t, orm.TextKey(orm.PrimaryOf(b, t.Key()))}
})

func Read(id string) orm.Query[Model] {
	return Models.Query().Where(orm.TextEq(Models.ID, id)).OrderBy(orm.TextAsc(Models.ID))
}
