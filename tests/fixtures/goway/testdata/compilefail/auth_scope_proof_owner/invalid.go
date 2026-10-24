package invalid

import (
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(member a.Member, scopes auth.AccessScopes[a.Other]) {
	_, _ = auth.NewScopedProof[a.Member, model.ID[a.Member]](a.MemberIdentity.ReferenceOf(member), auth.Authenticated, scopes)
}
