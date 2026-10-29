package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	s "foundry.test/goway/models/sensitive"
)

var _ = orm.ColumnEq(s.Accounts.Digest, "plaintext")
