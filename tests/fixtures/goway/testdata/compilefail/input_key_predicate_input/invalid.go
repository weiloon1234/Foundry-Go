package input_key_predicate_input

import m "foundry.test/goway/models/mutators"

var _ = m.KeyRecords.ID.Eq(m.FreshID{})
