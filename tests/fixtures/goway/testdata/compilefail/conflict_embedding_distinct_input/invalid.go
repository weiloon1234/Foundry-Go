package invalid

import (
	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct {
	orm.NullableInputText[n.Member, coremodels.LabelInput, coremodels.StoredLabel]
}

func (w wrapper) Set(v coremodels.StoredLabel) orm.Assignment[n.Member] {
	return n.Members.Name.Set(string(v))
}

var _ = orm.ConflictSet(wrapper{n.Members.Note}, coremodels.StoredLabel("stored"))
