package probe

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type M struct {
	ID    int64
	Value value.Nullable[password.Hash]
}
type Fields struct {
	orm.Table[M, int64]
	Value orm.NullableColumn[M, password.Hash]
}

var Table = orm.Define("probe", func(b orm.Builder[M], m *M) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.NullablePasswordOf(b, &m.Value)}
})
