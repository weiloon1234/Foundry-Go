package recovery_revision_identity

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	model "github.com/weiloon1234/Foundry-Go/model"
)

var _ = orm.Set(c.Records.Revision, model.ID[c.Record]{})
