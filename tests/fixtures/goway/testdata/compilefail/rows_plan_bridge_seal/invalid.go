package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

var _ = rows.FoundryNewDynamicSlot[models.User, string](sqlowner.Queries())
