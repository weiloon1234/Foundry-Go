package json_ordered_comparison

import c "foundry.test/goway/coremodels"

var _ = c.Records.Settings.Gt(c.Record{}.Settings)
