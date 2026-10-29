package json_draft_payload

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = orm.JSONSet(c.Records.Settings, value.JSON[string]{})
