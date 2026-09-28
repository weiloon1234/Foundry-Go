package cacheatomic

import "github.com/weiloon1234/Foundry-Go/fault"

// Limits shares the persistent adapter ceilings and defaults. Byte accounting
// follows each storage representation; both reject before replacing live data.
type Limits struct {
	MaxEntries    int
	MaxBytes      int64
	MaxValueBytes int
}

func DefaultLimits() Limits {
	return Limits{MaxEntries: 10000, MaxBytes: 64 << 20, MaxValueBytes: 1 << 20}
}
func ValidValueLimit(limit int) bool { return limit >= 20 && limit <= 64<<20 }
func (l Limits) Validate() error {
	if l.MaxEntries < 1 || l.MaxEntries > 1000000 || l.MaxBytes < 1 || l.MaxBytes > 1<<40 || !ValidValueLimit(l.MaxValueBytes) {
		return fault.New(fault.Invalid, "invalid persistent cache limits")
	}
	return nil
}
