package compilefail

import (
	"foundry.test/consumer/binarymodels"
	"foundry.test/consumer/models"
)

var _ = models.QueryUsers().Where(binarymodels.RecordFields().Body.Eq(binarymodels.Payload{1}))
