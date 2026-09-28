package idempotency

import (
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"time"
)

// Config bounds one store. MaxRetainedPerCaller counts all retained records,
// including expired ones, across its operations. Explicit pruning releases quota.
// New operations for one caller serialize on a transaction-scoped admission lock.
// Replays do not consume quota. MaxActive bounds this process's in-flight work.
type Config struct {
	Schema               string
	MaxActive            int
	MaxRetainedPerCaller int
	MaxKeyBytes          int
	MaxInputBytes        int
	MaxResultBytes       int
	Timeout              time.Duration
	DuplicateWait        time.Duration
	Retention            time.Duration
}

func DefaultConfig() Config {
	return Config{Schema: "public", MaxActive: 32, MaxRetainedPerCaller: 1000, MaxKeyBytes: MaxKeyBytes, MaxInputBytes: 1 << 18, MaxResultBytes: 1 << 18, Timeout: 15 * time.Second, DuplicateWait: time.Second, Retention: 7 * 24 * time.Hour}
}
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || c.MaxActive < 1 || c.MaxActive > 1024 || c.MaxRetainedPerCaller < 1 || c.MaxRetainedPerCaller > 100000 || c.MaxKeyBytes < MinKeyBytes || c.MaxKeyBytes > MaxKeyBytes || c.MaxInputBytes < 1 || c.MaxInputBytes > jsonwire.MaxBytes || c.MaxResultBytes < 1 || c.MaxResultBytes > jsonwire.MaxBytes || c.Timeout < time.Millisecond || c.Timeout > 5*time.Minute || c.DuplicateWait < time.Millisecond || c.DuplicateWait > c.Timeout || c.Retention < time.Hour || c.Retention > 365*24*time.Hour {
		return invalid("invalid idempotency resource bounds")
	}
	return nil
}
