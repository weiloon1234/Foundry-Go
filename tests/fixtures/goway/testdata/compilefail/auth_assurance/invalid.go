package invalid

import (
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func invalid() { _, _ = auth.NewProof(a.MemberIdentity.ReferenceOf(a.Member{}), "authenticated") }
