package catalog

import (
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type ProductColumns struct {
	orm.Table[Product, string]
	Code      orm.Primary[Product, string]
	Status    orm.Enum[Product, status.Status]
	CreatorID orm.Column[Product, model.ID[models.User]]
	Creator   orm.One[Product, models.User]
}

var Products = orm.Define("products", func(b orm.Builder[Product], p *Product) ProductColumns {
	table := orm.TableOf(b, &p.Code)
	return ProductColumns{Table: table, Code: orm.PrimaryOf(b, table.Key()), Status: orm.EnumOf(b, &p.Status), CreatorID: orm.IDOf(b, &p.CreatorID), Creator: orm.OneOf(b, &p.Creator)}
})
var ProductLinks = struct {
	Creator orm.BelongsTo[Product, models.User]
}{orm.BelongsToOf(Products.Creator, orm.ColumnExpr(Products.CreatorID), orm.KeyExpr(models.Users.ID))}
