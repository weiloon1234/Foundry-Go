package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func widen(slot rows.FoundryDynamicSlot[models.User, string]) rows.FoundryDynamicSlot[models.User, int] {
	return rows.FoundryDynamicSlot[models.User, int](slot)
}
