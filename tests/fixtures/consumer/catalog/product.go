// Package catalog exercises generated dependencies across consumer packages.
package catalog

import (
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:model table=products var=Products key=Code identity=false
type Product struct {
	Code      string `foundry:"member=Code,key-role=primary"`
	Status    status.Status
	CreatorID model.ID[models.User]
	Creator   relation.One[models.User]
}
