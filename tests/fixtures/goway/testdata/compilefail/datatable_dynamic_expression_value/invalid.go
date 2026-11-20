package invalid

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var slot = datatable.NewDynamicSlot[models.User, int]()

var _ = datatable.BindDynamic(slot, func(context.Context, struct{}) expr.Selected[models.User, string] {
	return expr.AsSelected(expr.Row(expr.Value(models.Users.Email)))
})
