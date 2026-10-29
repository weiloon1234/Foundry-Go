package input_required_null

import (
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.NullableInputTextSetNull(m.Members.Email)
