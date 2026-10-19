package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.LagOr(query.Value(models.UserFields().Email), 1, 123, query.WindowFor(models.QueryUsers()))
