package compilefail

import (
	"foundry.test/consumer/binarymodels"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = binarymodels.ViewSelection[binarymodels.Record]{Body: foundryquery.Value(binarymodels.RecordFields().Note)}
