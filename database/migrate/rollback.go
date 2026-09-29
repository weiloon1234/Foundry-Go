package migrate

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxRollbackSteps bounds one explicit rollback.
const MaxRollbackSteps = 1000

// RollbackResult lists migrations whose reversal committed, newest first.
// Interrupted names the attempted reversal lacking a successful confirmation;
// it may have committed when the returned database outcome is Unknown.
type RollbackResult struct {
	RolledBack  []Applied `json:"rolled_back"`
	Interrupted *Key      `json:"interrupted,omitempty"`
}

// RollbackPlan reports, without locking or changing anything, the last steps
// applied migrations that Rollback would reverse, newest first. It returns an
// error naming the obstacle when that rollback would be refused.
func (p *Postgres) RollbackPlan(ctx context.Context, steps int) ([]Applied, error) {
	history, err := p.history(ctx, p.db)
	if err != nil {
		return nil, err
	}
	report, err := p.inspectProgress(ctx, p.db, history)
	if err != nil {
		return nil, err
	}
	return p.rollbackSelection(report, history, steps)
}

// Rollback reverses the last steps applied migrations, newest first, each in
// its own transaction that runs the Down SQL and removes its history entry.
// It holds the migration lock and refuses before changing anything when
// history has drifted or awaits reconciliation, when a selected migration has
// no Down SQL, or when a migration outside the selection depends on one inside
// it. Earlier reversals stay committed if a later one fails. Rollback is an
// explicit operator action; it never runs during boot or migrate up.
func (p *Postgres) Rollback(ctx context.Context, steps int) (result RollbackResult, err error) {
	err = p.locked(ctx, func(session *database.Session) error {
		history, err := p.history(ctx, session)
		if err != nil {
			return err
		}
		report, err := p.inspectProgress(ctx, session, history)
		if err != nil {
			return err
		}
		selected, err := p.rollbackSelection(report, history, steps)
		if err != nil {
			return err
		}
		for _, applied := range selected {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := applied.Key
			result.Interrupted = &key
			item := p.registry.ordered[p.registry.byKey[key]]
			if err := p.reverse(ctx, session, item); err != nil {
				return p.failure(key, err)
			}
			result.RolledBack = append(result.RolledBack, applied)
			result.Interrupted = nil
		}
		return nil
	})
	return result, err
}

// rollbackSelection picks the newest steps entries (batch, then registry
// order, descending) and validates that every one can be reversed alone.
func (p *Postgres) rollbackSelection(report Report, history []Applied, steps int) ([]Applied, error) {
	if steps < 1 || steps > MaxRollbackSteps {
		return nil, fault.New(fault.Invalid, fmt.Sprintf("rollback steps must be between 1 and %d", MaxRollbackSteps))
	}
	if err := report.Check(); err != nil {
		return nil, err
	}
	for _, status := range report.Statuses {
		if status.State == Incomplete || status.Progress != nil {
			return nil, fault.New(fault.Conflict, "migration "+keyLabel(status.Key)+" is incomplete; reconcile it before rolling back")
		}
	}
	if steps > len(history) {
		return nil, fault.New(fault.Invalid, fmt.Sprintf("rollback of %d step(s) exceeds the %d applied migration(s)", steps, len(history)))
	}
	ordered := slices.Clone(history)
	slices.SortStableFunc(ordered, func(a, b Applied) int {
		if a.Batch != b.Batch {
			return int(b.Batch - a.Batch)
		}
		return p.registry.byKey[b.Key] - p.registry.byKey[a.Key]
	})
	selected := ordered[:steps]
	chosen := make(map[Key]bool, steps)
	for _, applied := range selected {
		chosen[applied.Key] = true
		item := p.registry.ordered[p.registry.byKey[applied.Key]]
		if len(item.down) == 0 {
			return nil, fault.New(fault.Invalid, "migration "+keyLabel(applied.Key)+" declares no Down SQL and cannot be rolled back")
		}
	}
	for _, applied := range ordered[steps:] {
		for _, required := range p.registry.ordered[p.registry.byKey[applied.Key]].entry.Requires {
			if chosen[required] {
				return nil, fault.New(fault.Conflict, "migration "+keyLabel(applied.Key)+" depends on "+keyLabel(required)+", which the rollback would reverse")
			}
		}
	}
	return selected, nil
}

// reverse runs Down and removes the history entry atomically.
func (p *Postgres) reverse(ctx context.Context, session *database.Session, item migration) error {
	return session.Transaction(ctx, func(tx *database.Tx) error {
		for _, statement := range item.down {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		removed, err := tx.Exec(ctx, "DELETE FROM "+p.table+" WHERE origin = $1 AND id = $2", string(item.entry.Key.Origin), string(item.entry.Key.ID))
		if err != nil {
			return err
		}
		if removed.RowsAffected != 1 {
			return fault.New(fault.Conflict, "migration history changed during rollback")
		}
		return nil
	})
}
