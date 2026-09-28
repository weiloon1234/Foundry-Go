// Package cacheint owns the canonical signed-integer cache representation.
package cacheint

import (
	"math"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxBytes includes the sign of the minimum signed 64-bit value.
const MaxBytes = 20

func Encode(value int64) []byte { return strconv.AppendInt(nil, value, 10) }
func Decode(data []byte) (int64, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return 0, fault.New(fault.Invalid, "invalid cached integer")
	}
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil || strconv.FormatInt(value, 10) != string(data) {
		return 0, fault.New(fault.Invalid, "cached integer must be canonical signed decimal")
	}
	return value, nil
}
func Add(value, delta int64) (int64, error) {
	if delta > 0 && value > math.MaxInt64-delta || delta < 0 && value < math.MinInt64-delta {
		return 0, fault.New(fault.Invalid, "cache counter overflow")
	}
	return value + delta, nil
}
