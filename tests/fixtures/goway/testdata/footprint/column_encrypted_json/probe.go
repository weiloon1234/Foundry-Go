package probe

import (
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type M struct {
	ID    int64
	Value encrypted.JSON[string]
}
type Fields struct {
	orm.Table[M, int64]
	Value orm.Encrypted[M, encrypted.JSON[string]]
}

var Table = orm.Define("probe", func(b orm.Builder[M], m *M) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.EncryptedJSONOf(b, &m.Value)}
})
