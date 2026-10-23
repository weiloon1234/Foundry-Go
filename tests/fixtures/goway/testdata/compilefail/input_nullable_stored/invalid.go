package input_nullable_stored

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

var _ = m.Members.Note.Set(c.StoredLabel("stored"))
