package codec

import (
	"database/sql/driver"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ID retains a model owner through UUID binding and decoding. Nil UUIDs remain
// values, not SQL NULL. Existence and required-primary rules belong to the ORM.
func ID[M any]() Codec[model.ID[M]] {
	return typed(TypeUUID, func(v model.ID[M]) (driver.Value, error) { return v.String(), nil }, func(source any) (model.ID[M], error) {
		v, err := text(source)
		if err != nil {
			return model.ID[M]{}, err
		}
		return model.ParseID[M](v)
	})
}

// Decimal uses exact text/integer driver representations. A floating-point
// source is never converted into an apparently exact decimal.
func Decimal() Codec[decimal.Decimal] {
	return typed(TypeDecimal, func(v decimal.Decimal) (driver.Value, error) { return v.String(), nil }, func(source any) (decimal.Decimal, error) {
		if v, ok := source.(int64); ok {
			return decimal.Parse(strconv.FormatInt(v, 10))
		}
		v, err := text(source)
		if err != nil {
			return decimal.Decimal{}, err
		}
		return decimal.Parse(v)
	})
}
