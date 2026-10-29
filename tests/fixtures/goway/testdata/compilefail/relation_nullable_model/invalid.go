package relation_nullable_model

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() {
	orm.BelongsToOf(models.Orders.Buyer, orm.NullableColumnExpr(models.Users.IntroducerID), orm.KeyExpr(models.Users.ID))
}
