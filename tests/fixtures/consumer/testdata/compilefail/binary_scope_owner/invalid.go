package compilefail

import (
	"foundry.test/consumer/binarymodels"
	"foundry.test/consumer/linkqueries"
)

var _ = linkqueries.Members.Query().Where(binarymodels.Records.Body.Eq(binarymodels.Payload{1}))
