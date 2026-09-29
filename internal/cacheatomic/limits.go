package cacheatomic

import (
	"crypto/sha256"
	"strings"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
)

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

// NamespacePrefix returns the physical address prefix shared by every plain
// cache entry of namespace, derived from cache.EntryKey's own address format.
func NamespacePrefix(namespace cache.Namespace) (string, error) {
	const name, logical = "flush", "flush"
	probe, err := cache.NewEntryKey(namespace, name, logical)
	if err != nil {
		return "", err
	}
	text := probe.String()
	suffix := len(":" + name + ":" + strings.Repeat("0", 2*sha256.Size))
	if len(text) <= suffix || !strings.Contains(text, ":"+namespace.Application+":"+namespace.Environment+":") {
		return "", fault.New(fault.Internal, "unexpected cache address format")
	}
	return text[:len(text)-suffix+1], nil
}
