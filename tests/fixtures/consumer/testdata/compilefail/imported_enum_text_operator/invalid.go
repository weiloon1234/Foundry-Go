package invalid

import (
	"foundry.test/consumer/catalog"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.Like(catalog.Products.Status, "available%")
