package input_text_number

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import m "foundry.test/goway/models/mutators"

var _ = orm.InputTextEq(m.Members.Email, 1)
