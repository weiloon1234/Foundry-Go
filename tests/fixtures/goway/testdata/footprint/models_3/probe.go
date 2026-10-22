package models_3

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

type Model1 struct {
	ID     int64
	Amount int64
	Name   value.Nullable[string]
}
type Fields1 struct {
	orm.Table[Model1, int64]
	Amount orm.Number[Model1, int64]
	Name   orm.NullableText[Model1, string]
}

var Models1 = orm.Define("models1", func(b orm.Builder[Model1], m *Model1) Fields1 {
	return Fields1{Table: orm.TableOf(b, &m.ID), Amount: orm.NumberOf(b, &m.Amount), Name: orm.NullableTextOf(b, &m.Name)}
})

type Model2 struct {
	ID     int64
	Amount int64
	Name   value.Nullable[string]
}
type Fields2 struct {
	orm.Table[Model2, int64]
	Amount orm.Number[Model2, int64]
	Name   orm.NullableText[Model2, string]
}

var Models2 = orm.Define("models2", func(b orm.Builder[Model2], m *Model2) Fields2 {
	return Fields2{Table: orm.TableOf(b, &m.ID), Amount: orm.NumberOf(b, &m.Amount), Name: orm.NullableTextOf(b, &m.Name)}
})
