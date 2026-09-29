package str

import (
	"strconv"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

var fileSizeUnits = [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// MaxFileSizePrecision bounds HumanFileSize fractional digits.
const MaxFileSizePrecision = 6

// HumanFileSize formats a byte count with binary (1024-based) IEC units and
// at most precision fractional digits, rounded half up without trailing
// zeros: 1536 with precision 1 is "1.5 KiB" and 1048575 with precision 0 is
// "1 MiB". Precision is clamped to 0–MaxFileSizePrecision. Digits are plain
// ASCII; use the numberformat package for locale presentation.
func HumanFileSize(size int64, precision int) string {
	precision = min(max(precision, 0), MaxFileSizePrecision)
	magnitude := uint64(size)
	if size < 0 {
		magnitude = -magnitude
	}
	unit := 0
	for unit < len(fileSizeUnits)-1 && magnitude >= uint64(1)<<(10*(unit+1)) {
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(size, 10) + " B"
	}
	value := scaledFileSize(size, unit, precision)
	// Rounding can reach the next unit, as 1048575 bytes becomes 1024 KiB.
	if unit < len(fileSizeUnits)-1 && value.Abs().Cmp(decimal.FromInt64(1024)) >= 0 {
		unit++
		value = scaledFileSize(size, unit, precision)
	}
	return value.String() + " " + fileSizeUnits[unit]
}

func scaledFileSize(size int64, unit, precision int) decimal.Decimal {
	// Both operands are exact int64 values and the scale is bounded, so the
	// division cannot exceed decimal.MaxDigits or fail.
	value, _ := decimal.FromInt64(size).Div(decimal.FromInt64(int64(1)<<(10*unit)), precision, decimal.HalfUp)
	return value
}
