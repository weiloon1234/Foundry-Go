package mutation_input_draft

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = orm.InputTextSet(m.Members.Email, c.StoredEmail("stored"))
