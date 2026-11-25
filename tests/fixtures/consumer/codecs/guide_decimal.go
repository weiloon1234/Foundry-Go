package codecs

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func taxAndShare(subtotal, total decimal.Decimal) error {
	// doc-example:begin supporting-apis-1
	rate, err := decimal.Parse("0.0825")
	if err != nil {
		return err
	}
	tax, err := subtotal.Mul(rate)
	if err != nil {
		return err
	}
	tax, err = tax.Round(2, decimal.HalfEven)
	if err != nil {
		return err
	}
	share, err := total.Div(decimal.FromInt64(3), 2, decimal.HalfUp)
	if err != nil {
		return err
	}
	// doc-example:end supporting-apis-1
	_ = tax
	_ = share
	return err
}
