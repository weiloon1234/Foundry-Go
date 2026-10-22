package column_nullable_text

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Model struct {
	ID   int64
	Name value.Nullable[string]
}
type Fields struct {
	orm.Table[Model, int64]
	Name orm.NullableText[Model, string]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{Table: orm.TableOf(b, &m.ID), Name: orm.NullableTextOf(b, &m.Name)}
})
