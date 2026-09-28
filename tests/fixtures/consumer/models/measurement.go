package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=measurements
type Measurement struct {
	ID     model.ID[Measurement]
	UserID model.ID[User]
	Amount value.Nullable[decimal.Decimal]
	Score  value.Nullable[float64]
	Label  string
}
