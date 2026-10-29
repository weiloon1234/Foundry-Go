package mutation_input_predicate

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = orm.InputTextEq(m.Members.Email, c.EmailInput{})
