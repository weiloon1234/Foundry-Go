package scalarqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type CalculationColumns struct {
	rows.Row[Calculation]
	ID            rows.Field[Calculation, int]
	Caption       rows.Field[Calculation, string]
	LineTotal     rows.Field[Calculation, decimal.Decimal]
	AmountWithTax rows.Field[Calculation, value.Nullable[decimal.Decimal]]
}

var Calculations = rows.Define(func(b rows.Builder[Calculation], r *Calculation) CalculationColumns {
	return CalculationColumns{Row: b.Row(), ID: rows.NumberOf(b, &r.ID), Caption: rows.TextOf(b, &r.Caption), LineTotal: rows.NumberOf(b, &r.LineTotal), AmountWithTax: rows.NullableNumberOf(b, &r.AmountWithTax)}
})
