package models

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Order is a persisted customer order.
//
//foundry:model table=orders var=Orders key=ID identity=false
type Order struct {
	ID         model.ID[Order] `foundry:"member=ID"`
	BuyerID    model.ID[User]
	TotalCents int64
	Buyer      relation.One[User]
}
