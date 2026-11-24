package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=measurements var=Measurements key=ID identity=false
type Measurement struct {
	ID     model.ID[Measurement] `foundry:"member=ID,key-role=primary"`
	UserID model.ID[User]
	Amount value.Nullable[decimal.Decimal] `foundry:"role=number"`
	Score  value.Nullable[float64]
	Label  string
}
