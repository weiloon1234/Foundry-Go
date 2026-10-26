package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.SubstringAt(models.UserFields().Email, models.UserFields().Email, models.UserFields().Email)
