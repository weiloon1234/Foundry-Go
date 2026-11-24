// Package scalarqueries exercises typed calculations from an independent consumer.
package scalarqueries

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Label string

//foundry:model table=calculation_samples var=Samples key=ID identity=false
type Sample struct {
	ID       int `foundry:"member=ID,key-role=primary-number"`
	Quantity int16
	Amount   decimal.Decimal `foundry:"role=number"`
	Score    float32
	Name     Label
	Note     value.Nullable[Label]
	Tax      value.Nullable[decimal.Decimal] `foundry:"role=number"`
}

type Calculation struct {
	ID            int
	Caption       string
	LineTotal     decimal.Decimal
	AmountWithTax value.Nullable[decimal.Decimal]
}
