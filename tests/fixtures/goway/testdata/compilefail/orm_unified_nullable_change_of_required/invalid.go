package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func f(ch orm.Changes[models.User]) {
	_, _ = orm.NullableChangeOf(ch, models.Users.Email)
}
