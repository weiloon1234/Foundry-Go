// Package catalog exercises generated dependencies across consumer packages.
package catalog

import (
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:model table=products primary=Code
type Product struct {
	Code      string
	Status    status.Status
	CreatorID model.ID[models.User]
	Creator   relation.One[models.User]
}

// CreatorDraft deliberately uses a generated type from another package.
func (p Product) CreatorDraft() models.UserDraft { return models.UserDraft{}.SetID(p.CreatorID) }

func (Product) DefineRelations() ProductRelationSet {
	return ProductRelationSet{Creator: query.BelongsTo(ProductFields().CreatorID, models.UserFields().ID)}
}
