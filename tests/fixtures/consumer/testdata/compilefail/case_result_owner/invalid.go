package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.When(models.UserFields().Age.Gt(0), models.GroupFields().Name)
