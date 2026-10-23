package probe

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type M struct {
	ID    int64
	Value password.Hash
}
type Fields struct {
	orm.Table[M, int64]
	Value orm.Column[M, password.Hash]
}

var Table = orm.Define("probe", func(b orm.Builder[M], m *M) Fields { return Fields{orm.TableOf(b, &m.ID), orm.PasswordOf(b, &m.Value)} })
