package idempotency

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// MaxPrune bounds rows removed by one Prune call or pruner statement.
const MaxPrune = 1000

// maxSweepBatches bounds the work of one automatic sweep; a backlog continues
// at the next interval instead of monopolizing an operation slot.
const maxSweepBatches = 16

// Prune explicitly removes at most limit completed, expired outcomes belonging
// to this application namespace. It never removes an uncommitted claim. Parallel
// callers skip locked rows. A removed key may execute again; schedule deliberately.
// The count is zero if the transaction did not have a confirmed commit.
// Stores started by Start/Module also prune automatically (Config.PruneInterval).
func (s *Store) Prune(ctx context.Context, limit int) (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > MaxPrune {
		return 0, invalid("idempotency pruning requires a limit from 1 to 1000")
	}
	var removed int64
	err := s.calls.Run(ctx, "prune idempotent outcomes", func(ctx context.Context) error {
		before, err := temporal.Now(s.db.Clock())
		if err != nil {
			return err
		}
		return s.db.Transaction(ctx, func(tx *database.Tx) error {
			// The statement is schema-qualified, so no search_path scope is needed.
			if _, err := tx.Exec(ctx, `SELECT pg_catalog.set_config('statement_timeout', $1, true)`, strconv.FormatInt(s.config.Timeout.Milliseconds(), 10)); err != nil {
				return err
			}
			result, err := tx.Exec(ctx, s.pruneStatement(), s.namespaceDigest(), before.UTC(), limit)
			if err != nil {
				return err
			}
			removed = result.RowsAffected
			return nil
		}, database.TxOptions{Isolation: database.ReadCommitted})
	})
	if err != nil {
		var transaction *database.Error
		if !errors.As(err, &transaction) || transaction.Outcome() != database.Committed {
			return 0, err
		}
	}
	return removed, err
}

// pruneStatement qualifies the validated schema identifier.
func (s *Store) pruneStatement() string {
	table := `"` + s.config.Schema + `".foundry_idempotency`
	return `WITH expired AS (
 SELECT id FROM ` + table + `
 WHERE namespace = $1 AND completed_at IS NOT NULL AND expires_at <= $2
 ORDER BY expires_at, id LIMIT $3 FOR UPDATE SKIP LOCKED
) DELETE FROM ` + table + ` AS outcome USING expired WHERE outcome.id = expired.id`
}

// prune is the store-owned background loop. Failures are logged with a
// redacted diagnostic and retried at the next interval; they never stop the
// application or retry a sweep immediately.
func (s *Store) prune(logger *slog.Logger, stop <-chan struct{}, exited chan<- struct{}) {
	defer close(exited)
	timer := time.NewTimer(s.config.PruneInterval)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		if err := s.sweep(stop); err != nil && logger != nil {
			_ = callback.Isolated("log idempotency pruning failure", func() error {
				logger.LogAttrs(context.Background(), slog.LevelWarn, "idempotency pruning failed", slog.Any("diagnostic", errordiag.Describe(err)))
				return nil
			})
		}
		timer.Reset(s.config.PruneInterval)
	}
}

// sweep removes bounded batches until a batch is not full or stop closes.
func (s *Store) sweep(stop <-chan struct{}) error {
	for range maxSweepBatches {
		select {
		case <-stop:
			return nil
		default:
		}
		removed, err := s.Prune(context.Background(), s.config.PruneBatch)
		if err != nil {
			select {
			case <-stop:
				// Shutdown canceled the sweep; that is not a pruning failure.
				return nil
			default:
			}
			return err
		}
		if removed < int64(s.config.PruneBatch) {
			return nil
		}
	}
	return nil
}
