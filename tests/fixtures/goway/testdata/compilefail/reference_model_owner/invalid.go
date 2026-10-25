package invalid

import (
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong model.Reference[a.Other, model.ID[a.Member]] = a.MemberIdentity.ReferenceOf(a.Member{})
