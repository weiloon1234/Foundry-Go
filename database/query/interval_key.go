package query

import (
	"math/big"
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

// PostgreSQL compares intervals using 30-day months and 24-hour days. Relation
// grouping must agree with that comparison while leaving model values intact.
// The combined value can exceed int64 even though each component is in range.
func intervalComparisonKey(v temporal.Interval) string {
	var total, days big.Int
	total.Mul(big.NewInt(int64(v.Months())), big.NewInt(30))
	days.Add(&total, big.NewInt(int64(v.Days())))
	total.Mul(&days, big.NewInt(int64(24*time.Hour/time.Microsecond)))
	total.Add(&total, big.NewInt(int64(v.Elapsed()/time.Microsecond)))
	return total.String()
}
