package input_key_predicate_input

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import m "foundry.test/goway/models/mutators"

var _ = orm.Eq(m.KeyRecords.ID, m.FreshID{})
