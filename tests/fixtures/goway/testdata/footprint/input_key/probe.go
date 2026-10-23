package input_key

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Model struct{ ID model.ID[Model] }
type FreshID struct{ Value model.ID[Model] }

func (Model) MutateID(input FreshID) (model.ID[Model], error) { return input.Value, nil }

type Fields struct {
	orm.Table[Model, model.ID[Model]]
	ID orm.InputKey[Model, FreshID, model.ID[Model]]
}

var Models = orm.Define("input_keys", func(b orm.Builder[Model], m *Model) Fields {
	table := orm.TableOf(b, &m.ID)
	return Fields{table, orm.InputKeyOf[Model, FreshID](b, table.Key(), nil)}
})
