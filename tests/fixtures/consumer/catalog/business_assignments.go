package catalog

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// CreatorAssignment supplies the creator's key as a User assignment.
// Example: models.Users.Insert(ctx, db, product.CreatorAssignment()).
func (p Product) CreatorAssignment() orm.Assignment[models.User] {
	return models.Users.ID.Set(p.CreatorID)
}
