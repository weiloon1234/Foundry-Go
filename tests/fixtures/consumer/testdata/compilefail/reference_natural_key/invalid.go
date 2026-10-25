package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong model.ID[linkqueries.Group] = orm.IdentityOf(linkqueries.Groups.Table, linkqueries.Groups.Key()).ReferenceOf(linkqueries.Group{}).Key()
