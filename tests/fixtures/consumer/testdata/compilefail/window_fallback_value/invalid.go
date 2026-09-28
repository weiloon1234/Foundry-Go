package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.LagOr(models.UserFields().Email.Value(), 1, 123, query.WindowFor(models.QueryUsers()))
