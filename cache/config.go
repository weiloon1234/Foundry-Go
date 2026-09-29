package cache

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Config bounds one application's typed cache access. Namespace is mandatory.
// MaxValueBytes bounds encoded data; custom codecs may allocate before that check.
type Config struct {
	Namespace       Namespace
	MaxKeyBytes     int
	MaxValueBytes   int
	MaxDeclarations int
	MaxBatchEntries int // Maximum batch input keys before deduplication; bounded by MaxBatchEntries.
	MaxTags         int // Maximum references in a tagged view/invalidation; bounded by cache.MaxTags.
	// MaxFills bounds concurrent Remember loaders across this store.
	MaxFills int
	// MaxFillWaiters bounds followers of each Remember loader (excluding its owner).
	MaxFillWaiters int
	// Timeout bounds each backend step (read, write, metadata) and every other
	// operation. It does not cap Remember loaders; LoadTimeout does.
	Timeout time.Duration
	// LoadTimeout bounds one Remember loader run, detached from the requesting
	// caller's cancellation. A call can narrow it with WithLoadTimeout.
	LoadTimeout time.Duration
}

// MaxLoadTimeout bounds how long a detached Remember loader can own its fill.
const MaxLoadTimeout = 24 * time.Hour

func DefaultConfig(namespace Namespace) Config {
	return Config{Namespace: namespace, MaxKeyBytes: 1024, MaxValueBytes: 1 << 20, MaxDeclarations: 256, MaxTags: 16, MaxBatchEntries: 64, MaxFills: 128, MaxFillWaiters: 256, Timeout: 5 * time.Second, LoadTimeout: 30 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxKeyBytes <= 0 || c.MaxKeyBytes > MaxKeyBytes || c.MaxValueBytes <= 0 || c.MaxDeclarations <= 0 || c.MaxBatchEntries <= 0 || c.MaxBatchEntries > MaxBatchEntries || c.MaxTags <= 0 || c.MaxTags > MaxTags || c.MaxFills <= 0 || c.MaxFillWaiters <= 0 || c.Timeout <= 0 || c.LoadTimeout <= 0 || c.LoadTimeout > MaxLoadTimeout {
		return fault.New(fault.Invalid, "invalid cache limits")
	}
	return nil
}
