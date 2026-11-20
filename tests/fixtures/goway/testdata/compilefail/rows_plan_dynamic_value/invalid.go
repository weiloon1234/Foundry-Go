package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type row struct {
	Age   int
	Email string
}

func bind(b rows.FoundryPlanBuilder[models.User, row], r *row, slot rows.FoundryDynamicSlot[models.User, string]) {
	rows.FoundryBindPlanDynamic(b, &r.Age, slot)
}
