package invalid

import (
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct{ orm.Text[n.Member, string] }

func (w wrapper) SetNull() orm.Assignment[n.Member] { return n.Members.Name.Set("redirected") }

var _ = orm.ConflictNull(wrapper{n.Members.Name})
