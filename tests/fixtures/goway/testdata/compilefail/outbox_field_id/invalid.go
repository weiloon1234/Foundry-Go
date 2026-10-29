package outbox_field_id

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	outbox "github.com/weiloon1234/Foundry-Go/outbox"
)

var _ = orm.ColumnSet(c.Records.MessageID, outbox.ID[string]{})
