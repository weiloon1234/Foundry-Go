package role_input_same

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct {
	ID   int64
	Name string
}
type Fields struct {
	orm.Table[Model, int64]
	Name orm.InputText[Model, string, string]
}

var Models = orm.Define("role_mutators", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.InputTextOf(b, &m.Name, func(s string) (string, error) { return s, nil })}
})

func Read(name string) orm.Query[Model] { return Models.Query().Where(Models.Name.Eq(name)) }
