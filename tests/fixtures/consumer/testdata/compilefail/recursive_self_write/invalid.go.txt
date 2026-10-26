package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var self query.RecursiveSelf[models.User]
var invalid = self.Create
