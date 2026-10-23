package mutator_nullable

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Model struct {
	ID   int64
	Name value.Nullable[string]
}

func invalid(b orm.Builder[Model], m *Model) {
	_ = orm.InputTextOf(b, &m.Name, func(v string) (string, error) { return v, nil })
}
