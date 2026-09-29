package audit

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// PruneBefore removes at most limit old entries in the selected area through the
// supplied transaction. Call explicitly from an authorized maintenance operation.
// It does not reset tables, hydrate payloads, dispatch observers or loop forever.
func (r *Recorder) PruneBefore(ctx context.Context, tx *database.Tx, cutoff temporal.DateTime, limit int) (int64, error) {
	area, err := r.area(ctx)
	if err != nil {
		return 0, err
	}
	if tx == nil || cutoff.IsZero() || limit < 1 || limit > query.MaxPageSize {
		return 0, fault.New(fault.Invalid, "audit pruning requires a transaction, cutoff and bounded row limit")
	}
	return auditstore.Prune(ctx, tx, string(area), cutoff, limit)
}

// CountBefore counts the entries of the selected area created before cutoff,
// the rows PruneBefore would remove, without reading their payloads.
func (r *Recorder) CountBefore(ctx context.Context, tx *database.Tx, cutoff temporal.DateTime) (int64, error) {
	area, err := r.area(ctx)
	if err != nil {
		return 0, err
	}
	if tx == nil || cutoff.IsZero() {
		return 0, fault.New(fault.Invalid, "audit counting requires a transaction and cutoff")
	}
	return auditstore.CountBefore(ctx, tx, string(area), cutoff)
}

// PruneRetention derives the explicit cutoff from supplied UTC time. Each day is
// 24 hours. Zero configured retention disables this helper; no background work
// or default deletion schedule is installed by constructing/registering a recorder.
func (r *Recorder) PruneRetention(ctx context.Context, tx *database.Tx, now temporal.DateTime, limit int) (int64, error) {
	if _, err := r.area(ctx); err != nil {
		return 0, err
	}
	cutoff, err := r.RetentionCutoff(now)
	if err != nil {
		return 0, err
	}
	return r.PruneBefore(ctx, tx, cutoff, limit)
}

// RetentionCutoff is now minus the configured retention in 24-hour days. Zero
// retention returns Missing; a zero now is invalid.
func (r *Recorder) RetentionCutoff(now temporal.DateTime) (temporal.DateTime, error) {
	if r == nil {
		return temporal.DateTime{}, fault.New(fault.Invalid, "audit recorder is not initialized")
	}
	if r.config.RetentionDays == 0 {
		return temporal.DateTime{}, fault.New(fault.Missing, "audit retention is not configured")
	}
	if now.IsZero() {
		return temporal.DateTime{}, fault.New(fault.Invalid, "audit retention requires a current time")
	}
	return now.Add(-time.Duration(r.config.RetentionDays) * 24 * time.Hour)
}

// PruneBefore repeatedly removes bounded batches of entries older than cutoff
// from the selected area, each batch in its own transaction, until a batch
// removes fewer than batch rows or ctx ends. It returns the rows removed by
// committed batches, including when a later batch fails; rerunning is safe.
// Call it only from an explicit, authorized maintenance operation.
func (s *Scope) PruneBefore(ctx context.Context, cutoff temporal.DateTime, batch int) (int64, error) {
	return s.prune(ctx, batch, func(tx *database.Tx, recorder *Recorder) (int64, error) {
		return recorder.PruneBefore(ctx, tx, cutoff, batch)
	})
}

// PruneRetention derives one cutoff from now and the configured retention, then
// prunes in bounded batches like PruneBefore. Zero retention returns Missing.
func (s *Scope) PruneRetention(ctx context.Context, now temporal.DateTime, batch int) (int64, error) {
	cutoff, err := s.RetentionCutoff(now)
	if err != nil {
		return 0, err
	}
	return s.PruneBefore(ctx, cutoff, batch)
}

// RetentionCutoff derives the configured retention cutoff for now, like
// Recorder.RetentionCutoff.
func (s *Scope) RetentionCutoff(now temporal.DateTime) (temporal.DateTime, error) {
	if s == nil {
		return temporal.DateTime{}, fault.New(fault.Invalid, "audit scope requires an initialized handle")
	}
	return s.recorder.RetentionCutoff(now)
}

// CountBefore counts, in one read-only transaction, the entries PruneBefore
// would remove from the selected area. It never reads payloads or deletes.
func (s *Scope) CountBefore(ctx context.Context, cutoff temporal.DateTime) (int64, error) {
	if s == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "audit scope requires an initialized handle and context")
	}
	var count int64
	err := s.db.Transaction(ctx, func(tx *database.Tx) error {
		return s.Within(ctx, tx, func(child *database.Tx, recorder *Recorder) error {
			var err error
			count, err = recorder.CountBefore(ctx, child, cutoff)
			return err
		})
	}, database.TxOptions{ReadOnly: true})
	return count, err
}

func (s *Scope) prune(ctx context.Context, batch int, step func(*database.Tx, *Recorder) (int64, error)) (int64, error) {
	if s == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "audit scope requires an initialized handle and context")
	}
	if batch < 1 || batch > query.MaxPageSize {
		return 0, fault.New(fault.Invalid, "audit pruning requires a bounded batch size")
	}
	if _, err := s.recorder.area(ctx); err != nil {
		return 0, err
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var removed int64
		err := s.db.Transaction(ctx, func(tx *database.Tx) error {
			return s.Within(ctx, tx, func(child *database.Tx, recorder *Recorder) error {
				var err error
				removed, err = step(child, recorder)
				return err
			})
		})
		if err != nil {
			return total, err
		}
		total += removed
		if removed < int64(batch) {
			return total, nil
		}
	}
}
