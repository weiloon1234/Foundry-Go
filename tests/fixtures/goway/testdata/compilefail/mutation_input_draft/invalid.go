package mutation_input_draft

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = m.Members.Email.Set(c.StoredEmail("stored"))
