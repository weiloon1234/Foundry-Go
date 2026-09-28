package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Equal(query.Count[models.User]().Value(), query.Count[models.User]().Value())
