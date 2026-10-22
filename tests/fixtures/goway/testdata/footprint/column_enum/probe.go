package column_enum

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct {
	ID    int64
	Value models.Status
}
type Fields struct {
	orm.Table[Model, int64]
	Value orm.Enum[Model, models.Status]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{Table: orm.TableOf(b, &m.ID), Value: orm.EnumOf(b, &m.Value)}
})
