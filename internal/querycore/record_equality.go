package querycore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"math/big"
	"time"
)

// EncodeModelKey normalizes signed zero while retaining driver representations.
func EncodeModelKey(raw driver.Value) (sqlvalue.Value, error) {
	if f, ok := raw.(float64); ok && f == 0 {
		raw = float64(0)
	}
	key, err := sqlvalue.Encode(raw)
	if err != nil {
		return sqlvalue.Value{}, safeerror.NewFault(fault.Invalid, "model key has no canonical database representation")
	}
	return key, nil
}

// RecordEqualityKey agrees with SQL equality, including PostgreSQL intervals.
func RecordEqualityKey(kind codec.ParameterType, raw driver.Value) (sqlvalue.Value, error) {
	if kind == codec.TypeInterval && raw != nil {
		v, err := codec.Interval().Decode(raw)
		if err != nil {
			return sqlvalue.Value{}, err
		}
		return sqlvalue.Value{Kind: "interval", Text: IntervalComparisonKey(v)}, nil
	}
	return EncodeModelKey(raw)
}

// IntervalComparisonKey uses PostgreSQL's 30-day months and 24-hour days.
func IntervalComparisonKey(v temporal.Interval) string {
	var total, days big.Int
	total.Mul(big.NewInt(int64(v.Months())), big.NewInt(30))
	days.Add(&total, big.NewInt(int64(v.Days())))
	total.Mul(&days, big.NewInt(int64(24*time.Hour/time.Microsecond)))
	total.Add(&total, big.NewInt(int64(v.Elapsed()/time.Microsecond)))
	return total.String()
}
