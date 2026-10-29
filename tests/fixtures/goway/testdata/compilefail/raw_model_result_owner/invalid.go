package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/goway/models"

func invalid() ([]models.User, error) {
	return orm.Raw(nil, nil, models.Orders.Table, "SELECT * FROM orders")
}
