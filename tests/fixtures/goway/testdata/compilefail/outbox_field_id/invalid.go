package outbox_field_id

import (
	c "foundry.test/goway/coremodels"
	outbox "github.com/weiloon1234/Foundry-Go/outbox"
)

var _ = c.Records.MessageID.Set(outbox.ID[string]{})
