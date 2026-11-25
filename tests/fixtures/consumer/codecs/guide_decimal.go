package codecs

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
)

// doc-example:begin supporting-apis-1
func taxAndShare(subtotal, total decimal.Decimal) error {
	rate, _ := decimal.Parse("0.0825")
	tax, err := subtotal.Mul(rate)
	if err != nil {
		return err
	}
	if err == nil {
		tax, err = tax.Round(2, decimal.HalfEven)
	}
	share, err := total.Div(decimal.FromInt64(3), 2, decimal.HalfUp)
	if err != nil {
		return err
	}
	_ = tax
	_ = share
	return err
}

// doc-example:end supporting-apis-1
