package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.NumericRange(query.WindowFor(models.QueryUsers()), models.UserFields().Age.Value()).Preceding("two")
