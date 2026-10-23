package recovery_revision_owner

import (
	c "foundry.test/goway/coremodels"
	challenge "github.com/weiloon1234/Foundry-Go/auth/challenge"
)

type Other struct{}

var _ = c.Records.Revision.Set(challenge.Revision[Other]{})
