package invalid

import (
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct {
	orm.InputText[n.Member, string, string]
}

func (w wrapper) Set(string) orm.Assignment[n.Member] {
	return orm.Set(n.Members.Body, []byte("redirected"))
}

var _ = orm.ConflictSet(wrapper{n.Members.Name}, "literal")
