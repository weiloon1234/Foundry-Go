package probe

import (
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type M struct {
	ID    int64
	Value encrypted.Text
}
type Fields struct {
	orm.Table[M, int64]
	Value orm.Encrypted[M, encrypted.Text]
}

var Table = orm.Define("probe", func(b orm.Builder[M], m *M) Fields {
	return Fields{orm.TableOf(b, &m.ID), orm.EncryptedTextOf(b, &m.Value)}
})
