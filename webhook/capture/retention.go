package capture

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const (
	pruneInterval = 10 * time.Minute
	pruneCatchUp  = time.Minute
	pruneBatch    = 1_000
	pruneBatches  = 50
	pruneTimeout  = 5 * time.Second
)

var errRetentionBusy = safeerror.NewFault(fault.Conflict, "another process holds the webhook capture retention lock")

// prunePass runs one scheduled pass and reports whether it stopped at its
// batch bound, so the next pass comes sooner.
func (s *Store) prunePass(ctx context.Context) bool {
	cutoff, err := s.retentionCutoff()
	if err != nil {
		return false
	}
	_, behind, err := s.prune(ctx, cutoff, true, pruneBatch, pruneBatches)
	if err != nil && !errors.Is(err, errRetentionBusy) && ctx.Err() == nil {
		s.logger.Warn("webhook capture retention pass failed", "schema", s.schema)
	}
	return behind
}

func (s *Store) retentionCutoff() (temporal.DateTime, error) {
	return temporal.NewDateTime(s.clock.Now().Add(-s.config.Retention))
}

// prune deletes rows received before cutoff, then, when capped, rows beyond
// MaxRows, oldest first. Every batch is one transaction holding the
// transaction-scoped retention lock; a replica that loses it stops with
// errRetentionBusy. It reports whether it stopped at maxBatches.
func (s *Store) prune(ctx context.Context, cutoff temporal.DateTime, capped bool, batch, maxBatches int) (int64, bool, error) {
	var removed int64
	batches := 0
	run := func(before temporal.DateTime) (bool, error) {
		for batches < maxBatches {
			n, err := s.pruneBatch(ctx, func(ctx context.Context, tx *database.Tx) (int64, error) {
				return store.DeleteBefore(ctx, tx, before, batch)
			})
			if err != nil {
				return false, err
			}
			removed += n
			batches++
			if n < int64(batch) {
				return true, nil
			}
		}
		return false, nil
	}
	done, err := run(cutoff)
	if err != nil || !done || !capped {
		return removed, !done && err == nil, err
	}
	var boundary temporal.DateTime
	var over bool
	if _, err := s.pruneBatch(ctx, func(ctx context.Context, tx *database.Tx) (int64, error) {
		newest, err := store.Overflow(ctx, tx, s.config.MaxRows)
		boundary, over = newest.Get()
		return 0, err
	}); err != nil || !over {
		return removed, false, err
	}
	after, err := boundary.Add(time.Microsecond)
	if err != nil {
		return removed, false, err
	}
	done, err = run(after)
	return removed, !done && err == nil, err
}

func (s *Store) pruneBatch(ctx context.Context, fn func(context.Context, *database.Tx) (int64, error)) (int64, error) {
	bounded, cancel := context.WithTimeout(ctx, pruneTimeout)
	defer cancel()
	var removed int64
	err := s.scope.Transaction(bounded, false, func(tx *database.Tx) error {
		locked, err := store.Lock(bounded, tx, s.schema)
		if err != nil {
			return err
		}
		if !locked {
			return errRetentionBusy
		}
		removed, err = fn(bounded, tx)
		return err
	})
	return removed, err
}

// pruneEstimate counts rows a prune would remove without deleting them.
func (s *Store) pruneEstimate(ctx context.Context, cutoff temporal.DateTime, capped bool) (int64, error) {
	var expired, retained int64
	err := s.scope.Transaction(ctx, true, func(tx *database.Tx) error {
		var err error
		if expired, err = store.CountBefore(ctx, tx, cutoff); err != nil || !capped {
			return err
		}
		total, err := store.Count(ctx, tx)
		retained = total - expired
		return err
	})
	return expired + max(0, retained-int64(s.config.MaxRows)), err
}
