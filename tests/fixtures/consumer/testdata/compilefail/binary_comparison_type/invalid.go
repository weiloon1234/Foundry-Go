package compilefail

import "foundry.test/consumer/binarymodels"

var _ = binarymodels.RecordFields().Body.Eq("text")
