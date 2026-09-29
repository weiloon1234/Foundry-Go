package idempotency

import (
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"time"
)

// Config bounds one store. MaxRetainedPerCaller counts a caller's unexpired
// outcomes across its operations; expired outcomes no longer consume quota.
// The check is a bounded indexed read after the claim and does not serialize a
// caller's operations, so concurrently executing new operations of one caller
// can each pass it: retention may exceed the quota by at most that concurrency.
// Replays do not consume quota. MaxActive bounds this process's in-flight work.
//
// PruneInterval schedules the store-owned pruner that Start (and Module) run;
// each sweep removes up to PruneBatch expired outcomes per statement. Zero
// disables automatic pruning; explicit Prune remains available.
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
	PruneInterval        time.Duration
	PruneBatch           int
}

func DefaultConfig() Config {
	return Config{Schema: "public", MaxActive: 32, MaxRetainedPerCaller: 1000, MaxKeyBytes: MaxKeyBytes, MaxInputBytes: 1 << 18, MaxResultBytes: 1 << 18, Timeout: 15 * time.Second, DuplicateWait: time.Second, Retention: 7 * 24 * time.Hour, PruneInterval: 10 * time.Minute, PruneBatch: 500}
}
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || c.MaxActive < 1 || c.MaxActive > 1024 || c.MaxRetainedPerCaller < 1 || c.MaxRetainedPerCaller > 100000 || c.MaxKeyBytes < MinKeyBytes || c.MaxKeyBytes > MaxKeyBytes || c.MaxInputBytes < 1 || c.MaxInputBytes > jsonwire.MaxBytes || c.MaxResultBytes < 1 || c.MaxResultBytes > jsonwire.MaxBytes || c.Timeout < time.Millisecond || c.Timeout > 5*time.Minute || c.DuplicateWait < time.Millisecond || c.DuplicateWait > c.Timeout || c.Retention < time.Hour || c.Retention > 365*24*time.Hour {
		return invalid("invalid idempotency resource bounds")
	}
	if c.PruneInterval != 0 && (c.PruneInterval < time.Second || c.PruneInterval > 24*time.Hour || c.PruneBatch < 1 || c.PruneBatch > MaxPrune) || c.PruneInterval == 0 && (c.PruneBatch < 0 || c.PruneBatch > MaxPrune) {
		return invalid("invalid idempotency pruning bounds")
	}
	return nil
}
