package lease

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Config bounds all acquisitions, waiters, live guards and owned callbacks in one
// manager. A scoped callback retains its slot until it actually exits, even after
// ownership loss. OperationTimeout also bounds each independent cleanup attempt.
type Config struct {
	Namespace                               keyspace.Namespace
	MaxActive, MaxDeclarations, MaxKeyBytes int
	OperationTimeout, MaxWait, PollInterval time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxActive: 128, MaxDeclarations: 256, MaxKeyBytes: 1024, OperationTimeout: 5 * time.Second, MaxWait: 5 * time.Minute, PollInterval: 50 * time.Millisecond}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxActive <= 0 || c.MaxDeclarations <= 0 || c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.OperationTimeout <= 0 || c.MaxWait <= 0 || c.PollInterval <= 0 || c.PollInterval > c.MaxWait {
		return fault.New(fault.Invalid, "invalid lease limits")
	}
	return nil
}

// Validity deliberately subtracts elapsed command time plus a drift/expiry margin.
func validity(ttl time.Duration) time.Duration { return ttl - ttl/100 - 2*time.Millisecond }
