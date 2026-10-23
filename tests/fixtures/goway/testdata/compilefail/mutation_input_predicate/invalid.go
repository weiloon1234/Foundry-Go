package mutation_input_predicate

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = m.Members.Email.Eq(c.EmailInput{})
