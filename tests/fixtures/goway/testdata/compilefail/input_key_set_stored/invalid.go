package input_key_set_stored

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = orm.InputKeySet(m.KeyRecords.ID, model.ID[m.KeyRecord]{})
