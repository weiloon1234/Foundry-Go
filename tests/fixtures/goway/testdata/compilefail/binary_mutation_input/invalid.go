package binary_mutation_input

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = orm.InputColumnSet(m.Members.Body, c.StoredBody{1})
