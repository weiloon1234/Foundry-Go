package idempotency

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Prune explicitly removes at most limit completed, expired outcomes belonging
// to this application namespace. It never removes an uncommitted claim. Parallel
// callers skip locked rows. A removed key may execute again; schedule deliberately.
// The count is zero if the transaction did not have a confirmed commit.
// Repository tests retain their data and never invoke production pruning.
func (s *Store) Prune(ctx context.Context, limit int) (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 1000 {
		return 0, invalid("idempotency pruning requires a limit from 1 to 1000")
	}
	var removed int64
	err := s.calls.Run(ctx, "prune idempotent outcomes", func(ctx context.Context) error {
		before, err := temporal.Now(s.db.Clock())
		if err != nil {
			return err
		}
		return s.db.Transaction(ctx, func(tx *database.Tx) error {
			if err := s.limits(ctx, tx); err != nil {
				return err
			}
			return s.scoped(ctx, tx, func(child *database.Tx) error {
				result, err := child.Exec(ctx, pruneStatement, s.namespaceDigest(), before.UTC(), limit)
				if err != nil {
					return err
				}
				removed = result.RowsAffected
				return nil
			})
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

const pruneStatement = `WITH expired AS (
 SELECT id FROM foundry_idempotency
 WHERE namespace = $1 AND completed_at IS NOT NULL AND expires_at <= $2
 ORDER BY expires_at, id LIMIT $3 FOR UPDATE SKIP LOCKED
) DELETE FROM foundry_idempotency AS outcome USING expired WHERE outcome.id = expired.id`
