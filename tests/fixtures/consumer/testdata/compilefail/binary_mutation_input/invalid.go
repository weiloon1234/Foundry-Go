package compilefail

import "foundry.test/consumer/binarymodels"

var _ = binarymodels.RecordDraft{}.SetBody(binarymodels.Payload{1})
