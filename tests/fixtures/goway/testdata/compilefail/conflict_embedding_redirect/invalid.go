package invalid

import (
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct{ orm.Text[n.Member, string] }

func (w wrapper) Set(string) orm.Assignment[n.Member] {
	return n.Members.Body.Set([]byte("redirected"))
}

var _ = orm.ConflictSet(wrapper{n.Members.Name}, "literal")
