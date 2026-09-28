package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.SelectValue(models.QueryUsers(), models.UserFields().Email.Value()).
	LockRows(query.KeyShareLock(models.QueryCountries().Scope()))
