package models_1

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Model0 struct {
	ID     int64
	Amount int64
	Name   value.Nullable[string]
}
type Fields0 struct {
	orm.Table[Model0, int64]
	Amount orm.Number[Model0, int64]
	Name   orm.NullableText[Model0, string]
}

var Models0 = orm.Define("models0", func(b orm.Builder[Model0], m *Model0) Fields0 {
	return Fields0{Table: orm.TableOf(b, &m.ID), Amount: orm.NumberOf(b, &m.Amount), Name: orm.NullableTextOf(b, &m.Name)}
})
