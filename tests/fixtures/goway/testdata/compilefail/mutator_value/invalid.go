package mutator_value

import "github.com/weiloon1234/Foundry-Go/database/orm"

type Model struct {
	ID   int64
	Name string
}

func invalid(b orm.Builder[Model], m *Model) {
	_ = orm.InputTextOf(b, &m.Name, func(v int) (int, error) { return v, nil })
}
