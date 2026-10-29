package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

func wrong() {
	_ = orm.EachChunked(nil, nil, models.Users.Query(), 2, func(models.Order) error { return nil })
}
