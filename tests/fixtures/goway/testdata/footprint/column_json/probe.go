package column_json

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Model struct {
	ID    int64
	Value value.JSON[string]
}
type Fields struct {
	orm.Table[Model, int64]
	Value orm.JSON[Model, string]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Value)}
})
