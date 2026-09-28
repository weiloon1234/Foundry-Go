package compilefail

import (
	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ query.KeyField[binarymodels.Record, binarymodels.Payload]
