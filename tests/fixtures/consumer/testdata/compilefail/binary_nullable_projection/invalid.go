package compilefail

import "foundry.test/consumer/binarymodels"

var _ = binarymodels.ViewSelection[binarymodels.Record]{Body: binarymodels.RecordFields().Note.Value()}
