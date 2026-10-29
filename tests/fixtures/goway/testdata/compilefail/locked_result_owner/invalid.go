package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

func invalid() ([]models.Order, error) {
	return orm.AllLocked(nil, nil, orm.Lock(models.Users.Query()))
}
