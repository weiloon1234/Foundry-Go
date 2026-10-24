package invalid

import (
	"foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.ForceDeleteUsing(models.Users.Query(), h.Members.Soft, rows.From(models.Users.Query()))
