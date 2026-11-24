package catalog

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var ProductLinks = struct {
	Creator orm.BelongsTo[Product, models.User]
}{orm.BelongsToOf(Products.Creator, Products.CreatorID, models.Users.ID)}
