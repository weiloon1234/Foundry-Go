package compilefail

import (
	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ orm.KeyColumn[binarymodels.Record, binarymodels.Payload]
