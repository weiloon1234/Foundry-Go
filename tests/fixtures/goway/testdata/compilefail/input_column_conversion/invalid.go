package input_column_conversion

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.InputText[m.Member, c.StoredEmail, c.StoredEmail](m.Members.Email)
