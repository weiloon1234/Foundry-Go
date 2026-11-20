package aggregate_temporal_avg

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.Avg(models.Users.Birthday)
