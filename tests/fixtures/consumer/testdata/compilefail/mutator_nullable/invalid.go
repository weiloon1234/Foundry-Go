package mutator_nullable

import (
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(b orm.Builder[mutatorqueries.Member], m *mutatorqueries.Member) {
	_ = orm.InputTextOf(b, &m.Nickname, func(v string) (string, error) { return v, nil })
}
