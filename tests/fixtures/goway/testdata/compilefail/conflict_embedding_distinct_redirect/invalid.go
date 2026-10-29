package invalid

import (
	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type wrapper struct {
	orm.NullableInputTextAdapter[n.Member, coremodels.LabelInput, coremodels.StoredLabel]
}

func (w wrapper) Set(coremodels.LabelInput) orm.Assignment[n.Member] {
	return orm.InputTextSet(n.Members.Name, "redirected")
}

var _ = orm.ConflictSet(wrapper{orm.NullableInputTextExpr(n.Members.Note)}, coremodels.LabelInput{Text: "literal"})
