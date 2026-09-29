package cacheatomic

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

// MaxPrune bounds the records one prune batch removes. Adapters re-export it.
const MaxPrune = 256

// DefaultPruneInterval is how often a started adapter reclaims expired records.
const DefaultPruneInterval = time.Minute

// ValidPruneInterval accepts zero (automatic pruning disabled) or 1s to 24h.
func ValidPruneInterval(interval time.Duration) bool {
	return interval == 0 || interval >= time.Second && interval <= 24*time.Hour
}

// reconcileGap rate-limits on-demand reconciliation when a write finds the
// estimate at capacity, so a full cache rejects writes without rescanning.
const reconcileGap = time.Second

// Usage is a backend-owned estimate of stored records and bytes. Writers apply
// their own deltas without scanning storage; Reconcile replaces the estimate
// with a counted snapshot. Other processes' writes become visible only at the
// next reconciliation, so capacity is approximate between prune passes.
type Usage struct {
	entries, bytes atomic.Int64
	// reconciled is the wall time (UnixNano) of the last count, zero before it.
	reconciled atomic.Int64
	// claimed is the wall time (UnixNano) of the last on-demand reclamation.
	claimed atomic.Int64
}

// Add applies one writer's delta, never letting the estimate go negative.
func (u *Usage) Add(entries, bytes int64) {
	if u.entries.Add(entries) < 0 {
		u.entries.Store(0)
	}
	if u.bytes.Add(bytes) < 0 {
		u.bytes.Store(0)
	}
}

// Reconcile replaces the estimate with a counted snapshot.
func (u *Usage) Reconcile(entries, bytes int64) {
	u.entries.Store(entries)
	u.bytes.Store(bytes)
	u.reconciled.Store(time.Now().UnixNano())
}

// Snapshot returns the current estimate.
func (u *Usage) Snapshot() (entries, bytes int64) { return u.entries.Load(), u.bytes.Load() }

// Counted reports whether any reconciliation has happened.
func (u *Usage) Counted() bool { return u.reconciled.Load() != 0 }

// Fits reports whether a write of size bytes fits the limits. replaced is the
// size of an existing physical record at the same address, or -1 when none.
func (u *Usage) Fits(limits Limits, replaced, size int64) bool {
	if size > limits.MaxBytes {
		return false
	}
	entries, bytes := u.Snapshot()
	if replaced < 0 {
		return entries < int64(limits.MaxEntries) && bytes <= limits.MaxBytes-size
	}
	return bytes-replaced <= limits.MaxBytes-size
}

// ReconcileDue claims the right to reclaim/recount for a write at capacity. At
// most one claim succeeds per reconcileGap; others reject from the estimate.
// Scheduled prune passes do not consume the claim.
func (u *Usage) ReconcileDue() bool {
	last := u.claimed.Load()
	now := time.Now().UnixNano()
	if last != 0 && now-last < int64(reconcileGap) {
		return false
	}
	return u.claimed.CompareAndSwap(last, now)
}

// Reclaimed releases the claim after an on-demand pass that removed records,
// so only unproductive passes (a cache full of live data) are rate-limited.
func (u *Usage) Reclaimed(result PruneResult) {
	if result.Removed > 0 {
		u.claimed.Store(0)
	}
}

// PruneResult reports one bounded prune pass.
type PruneResult struct {
	// Removed counts expired records (and file orphans) deleted by this pass.
	Removed int
	// Corrupt counts unreadable records that were skipped. Reads treat them as
	// misses and writes replace them; they still occupy capacity until then.
	Corrupt int
	// Unrecognized counts foreign files skipped in an owned file-cache root.
	Unrecognized int
	// Entries and Bytes are the stored records counted after removal.
	Entries int64
	Bytes   int64
	// Complete reports that the pass counted all storage. Every pass replaces
	// the adapter's usage estimate; an incomplete one with a lower bound.
	Complete bool
}

// CapacityError is the shared quota rejection after reclaiming expiry failed.
func CapacityError(adapter string) error {
	return fault.New(fault.Invalid, adapter+" capacity reached")
}

// RunPruner calls pass every interval until stop closes. A tick repeats full
// batches (bounded) so a backlog drains without waiting for later ticks.
// Failures are reported through logger as redacted diagnostics only.
func RunPruner(ctx context.Context, interval time.Duration, logger *slog.Logger, name string, pass func(context.Context) (PruneResult, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for range 16 {
			result, err := pass(ctx)
			if err != nil {
				if ctx.Err() == nil && logger != nil {
					diagnostic := errordiag.Describe(err)
					_ = callback.Isolated("log cache prune failure", func() error {
						logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelWarn, "cache prune failed", slog.String("adapter", name), slog.Any("diagnostic", diagnostic))
						return nil
					})
				}
				break
			}
			if result.Removed < MaxPrune {
				break
			}
		}
	}
}
