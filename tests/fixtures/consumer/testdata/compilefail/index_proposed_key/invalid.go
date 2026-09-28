package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.OnConflictKeys[models.User](models.UserFieldsAt(models.QueryUsers().ConflictRows().Proposed()).Email.Group())
