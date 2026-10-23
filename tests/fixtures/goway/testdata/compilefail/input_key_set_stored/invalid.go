package input_key_set_stored

import (
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = m.KeyRecords.ID.Set(model.ID[m.KeyRecord]{})
