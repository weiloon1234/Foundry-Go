package compilefail

import "foundry.test/consumer/binarymodels"

var _ = binarymodels.Records.Body.Set(binarymodels.Payload{1})
