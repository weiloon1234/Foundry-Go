package mutator_owner

import (
	c "foundry.test/goway/coremodels"
	u "foundry.test/goway/models"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.InputText[u.User, c.EmailInput, c.StoredEmail] = m.Members.Email
