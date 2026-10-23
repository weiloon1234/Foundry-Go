package correlation_scope_conversion

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.Inner[models.User, models.Order](rows.Correlation[models.User, models.Order]{})
