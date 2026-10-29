package input_nullable_stored

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = orm.NullableInputTextSet(m.Members.Note, c.StoredLabel("stored"))
