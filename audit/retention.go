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

// PruneRetention derives the explicit cutoff from supplied UTC time. Each day is
// 24 hours. Zero configured retention disables this helper; no background work
// or default deletion schedule is installed by constructing/registering a recorder.
func (r *Recorder) PruneRetention(ctx context.Context, tx *database.Tx, now temporal.DateTime, limit int) (int64, error) {
	if _, err := r.area(ctx); err != nil {
		return 0, err
	}
	if r.config.RetentionDays == 0 {
		return 0, fault.New(fault.Missing, "audit retention is not configured")
	}
	if now.IsZero() {
		return 0, fault.New(fault.Invalid, "audit retention requires a current time")
	}
	cutoff, err := now.Add(-time.Duration(r.config.RetentionDays) * 24 * time.Hour)
	if err != nil {
		return 0, err
	}
	return r.PruneBefore(ctx, tx, cutoff, limit)
}
