package invalid

import (
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct {
	orm.InputTextAdapter[n.Member, string, string]
}

func (w wrapper) Set(int) orm.Assignment[n.Member] {
	return orm.InputTextSet(n.Members.Name, "redirected")
}

var _ = orm.ConflictSet(wrapper{orm.InputTextExpr(n.Members.Name)}, int(123))
