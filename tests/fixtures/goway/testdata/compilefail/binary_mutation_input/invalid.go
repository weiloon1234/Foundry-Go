package binary_mutation_input

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = m.Members.Body.Set(c.StoredBody{1})
