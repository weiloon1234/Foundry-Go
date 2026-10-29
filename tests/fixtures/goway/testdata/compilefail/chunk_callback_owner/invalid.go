package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
)

func wrong() {
	_ = orm.Chunk(nil, nil, models.Users.Query(), 2, func([]models.Order) error { return nil })
}
