package invalid

import (
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct {
	orm.InputText[n.Member, string, string]
}

func (w wrapper) Set(int) orm.Assignment[n.Member] { return n.Members.Name.Set("redirected") }

var _ = orm.ConflictSet(wrapper{n.Members.Name}, int(123))
