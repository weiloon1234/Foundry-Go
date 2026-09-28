// Package scalarqueries exercises typed calculations from an independent consumer.
package scalarqueries

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Label string

//foundry:model table=calculation_samples primary=ID
type Sample struct {
	ID       int
	Quantity int16
	Amount   decimal.Decimal
	Score    float32
	Name     Label
	Note     value.Nullable[Label]
	Tax      value.Nullable[decimal.Decimal]
}

//foundry:projection
type Calculation struct {
	ID            int
	Caption       string
	LineTotal     decimal.Decimal
	AmountWithTax value.Nullable[decimal.Decimal]
}
