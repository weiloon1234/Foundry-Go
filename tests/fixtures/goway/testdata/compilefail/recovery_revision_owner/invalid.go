package recovery_revision_owner

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	challenge "github.com/weiloon1234/Foundry-Go/auth/challenge"
)

type Other struct{}

var _ = orm.ColumnSet(c.Records.Revision, challenge.Revision[Other]{})
