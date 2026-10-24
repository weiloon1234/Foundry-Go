package mutator_value

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(b orm.Builder[mutatorqueries.Member], m *mutatorqueries.Member) {
	_ = orm.InputTextOf(b, &m.Email, func(v int) (int, error) { return v, nil })
}
