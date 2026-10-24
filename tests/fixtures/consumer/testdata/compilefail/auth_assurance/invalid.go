package invalid

import (
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad() {
	_, _ = auth.NewProof(profiles.ProfileIdentity.ReferenceOf(profiles.Profile{}), "authenticated")
}
