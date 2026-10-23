package mutation_input_nullable

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.NullableInputText[m.Member, c.StoredLabel, c.StoredLabel] = m.Members.Note
