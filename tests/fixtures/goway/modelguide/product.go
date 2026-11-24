// Package modelguide supplies complete generated declarations for the guides.
package modelguide

import (
	"time"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=guide_products var=Products key=ID
type Product struct {
	ID        model.ID[Product]
	OwnerID   model.ID[models.User]
	Status    models.Status
	Price     decimal.Decimal                 `foundry:"role=number"`
	Discount  value.Nullable[decimal.Decimal] `foundry:"role=number"`
	CreatedAt time.Time                       `foundry:"role=created-at"`
	UpdatedAt time.Time                       `foundry:"role=updated-at"`
}
