package catalog

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// CreatorAssignment supplies the creator's key as a User assignment.
// Example: orm.Insert(ctx, db, models.Users.Table, product.CreatorAssignment()).
func (p Product) CreatorAssignment() orm.Assignment[models.User] {
	return orm.KeySet(models.Users.ID, p.CreatorID)
}
