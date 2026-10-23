package aggregate_slot_declaration

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

type Model struct {
	ID    int64
	Count relation.Value[int64]
}
type Fields struct {
	orm.Table[Model, int64]
	Count orm.Value[Model, int64]
}

var Models = orm.Define("slot_models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.ValueOf(b, &m.Count)}
})
