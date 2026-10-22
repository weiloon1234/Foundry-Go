package column_number

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type Model struct {
	ID     int64
	Amount int64
}
type Fields struct {
	orm.Table[Model, int64]
	Amount orm.Number[Model, int64]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{Table: orm.TableOf(b, &m.ID), Amount: orm.NumberOf(b, &m.Amount)}
})
